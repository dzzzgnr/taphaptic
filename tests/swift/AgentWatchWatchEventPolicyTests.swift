import Foundation
import XCTest

final class AgentWatchWatchEventPolicyTests: XCTestCase {
    private let config = AgentWatchWatchEventPolicyConfig.defaults

    func testSameEventIDDoesNotReplayWhileActive() {
        let now = Date(timeIntervalSince1970: 1_772_713_000)
        let event = makeEvent(id: 1_772_712_975_044, type: .completed, createdAt: now)

        let first = AgentWatchWatchEventPolicy.apply(
            event: event,
            state: makeInitialState(),
            now: now,
            config: config
        )
        let second = AgentWatchWatchEventPolicy.apply(
            event: event,
            state: first.state,
            now: now.addingTimeInterval(2),
            config: config
        )

        XCTAssertEqual(first.state.lastSeenEventID, event.id)
        XCTAssertEqual(second.action, .noChange)
    }

    func testStaleEventIsMarkedSeenAndIgnored() {
        let now = Date(timeIntervalSince1970: 1_772_713_000)
        let staleEvent = makeEvent(
            id: 1_772_712_975_099,
            type: .completed,
            createdAt: now.addingTimeInterval(-(config.staleEventMaxAgeSeconds + 5))
        )

        let result = AgentWatchWatchEventPolicy.apply(
            event: staleEvent,
            state: makeInitialState(),
            now: now,
            config: config
        )

        XCTAssertEqual(result.state.lastSeenEventID, staleEvent.id)
        XCTAssertEqual(result.action, .showPending)
    }

    func testNewEventReplacesActiveTransientImmediately() {
        let now = Date(timeIntervalSince1970: 1_772_713_000)
        let completedEvent = makeEvent(id: 1_772_712_975_100, type: .completed, createdAt: now)
        let failureEvent = makeEvent(id: 1_772_712_975_101, type: .failed, createdAt: now.addingTimeInterval(1))

        let first = AgentWatchWatchEventPolicy.apply(
            event: completedEvent,
            state: makeInitialState(),
            now: now,
            config: config
        )
        let second = AgentWatchWatchEventPolicy.apply(
            event: failureEvent,
            state: first.state,
            now: now.addingTimeInterval(1.5),
            config: config
        )

        guard case .showEvent(let secondPresentation) = second.action else {
            return XCTFail("Expected second event to replace active transient immediately.")
        }

        XCTAssertEqual(secondPresentation.eventID, failureEvent.id)
        XCTAssertEqual(secondPresentation.eventType, .failed)
        XCTAssertEqual(second.state.activeEventID, failureEvent.id)
    }

    func testCompletedSequenceTimingContract() {
        let now = Date(timeIntervalSince1970: 1_772_713_000)
        let event = makeEvent(id: 1_772_712_975_200, type: .completed, createdAt: now)

        let result = AgentWatchWatchEventPolicy.apply(
            event: event,
            state: makeInitialState(),
            now: now,
            config: config
        )

        guard case .showEvent(let presentation) = result.action else {
            return XCTFail("Expected completed event to produce presentation.")
        }

        XCTAssertTrue(presentation.animateCompleted)
        XCTAssertEqual(
            presentation.hapticPlan,
            .immediate(.completed)
        )
        XCTAssertEqual(
            presentation.expiresAt.timeIntervalSince(now),
            config.completedAnimationSeconds + config.transientDisplayWindowSeconds,
            accuracy: 0.0001
        )
    }

    func testPendingAppearsOnlyAfterTransientExpiry() {
        let now = Date(timeIntervalSince1970: 1_772_713_000)
        let event = makeEvent(id: 1_772_712_975_300, type: .completed, createdAt: now)

        let shown = AgentWatchWatchEventPolicy.apply(
            event: event,
            state: makeInitialState(),
            now: now,
            config: config
        )

        let beforeExpiry = AgentWatchWatchEventPolicy.applyPending(
            state: shown.state,
            now: now.addingTimeInterval(config.completedAnimationSeconds + config.transientDisplayWindowSeconds - 0.2)
        )
        let afterExpiry = AgentWatchWatchEventPolicy.applyPending(
            state: shown.state,
            now: now.addingTimeInterval(config.completedAnimationSeconds + config.transientDisplayWindowSeconds + 0.2)
        )

        XCTAssertEqual(beforeExpiry.action, .noChange)
        XCTAssertEqual(afterExpiry.action, .showPending)
        XCTAssertNil(afterExpiry.state.activeEventID)
    }

    func testSeenEventAfterExpiryShowsPending() {
        let now = Date(timeIntervalSince1970: 1_772_713_000)
        let event = makeEvent(id: 1_772_712_975_320, type: .completed, createdAt: now)

        let shown = AgentWatchWatchEventPolicy.apply(
            event: event,
            state: makeInitialState(),
            now: now,
            config: config
        )

        let afterExpiry = AgentWatchWatchEventPolicy.apply(
            event: event,
            state: shown.state,
            now: now.addingTimeInterval(config.completedAnimationSeconds + config.transientDisplayWindowSeconds + 0.2),
            config: config
        )

        XCTAssertEqual(afterExpiry.action, .showPending)
        XCTAssertNil(afterExpiry.state.activeEventID)
    }

    private func makeInitialState() -> AgentWatchWatchEventPolicyState {
        AgentWatchWatchEventPolicyState(
            lastSeenEventID: 0,
            activeEventID: nil,
            activeEventExpiresAt: nil
        )
    }

    private func makeEvent(id: Int64, type: AgentWatchEventType, createdAt: Date) -> AgentWatchEvent {
        AgentWatchEvent(
            id: id,
            type: type,
            createdAt: createdAt,
            source: "test",
            title: nil,
            body: nil
        )
    }
}
