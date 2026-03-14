import Foundation

struct TaphapticWatchEventPolicyConfig: Equatable {
    let completedAnimationSeconds: TimeInterval
    let transientDisplayWindowSeconds: TimeInterval
    let staleEventMaxAgeSeconds: TimeInterval

    static let defaults = TaphapticWatchEventPolicyConfig(
        completedAnimationSeconds: 1.25,
        transientDisplayWindowSeconds: 3,
        staleEventMaxAgeSeconds: 20
    )
}

struct TaphapticWatchEventPolicyState: Equatable {
    var lastSeenEventID: Int64
    var activeEventID: Int64?
    var activeEventExpiresAt: Date?

    func isActive(at now: Date) -> Bool {
        guard activeEventID != nil else {
            return false
        }
        guard let activeEventExpiresAt else {
            return false
        }
        return activeEventExpiresAt > now
    }

    mutating func clearExpiredActiveState(at now: Date) {
        if isActive(at: now) {
            return
        }
        activeEventID = nil
        activeEventExpiresAt = nil
    }
}

enum TaphapticWatchEventHapticPlan: Equatable {
    case none
    case immediate(TaphapticEventType)
    case delayed(TaphapticEventType, TimeInterval)
}

struct TaphapticWatchEventPresentation: Equatable {
    let eventID: Int64
    let eventType: TaphapticEventType
    let detailText: String
    let animateCompleted: Bool
    let hapticPlan: TaphapticWatchEventHapticPlan
    let expiresAt: Date
}

enum TaphapticWatchEventPolicyAction: Equatable {
    case noChange
    case showPending
    case showEvent(TaphapticWatchEventPresentation)
}

struct TaphapticWatchEventPolicyResult: Equatable {
    let state: TaphapticWatchEventPolicyState
    let action: TaphapticWatchEventPolicyAction
}

enum TaphapticWatchEventPolicy {
    static func apply(
        event: TaphapticEvent,
        state: TaphapticWatchEventPolicyState,
        now: Date,
        config: TaphapticWatchEventPolicyConfig = .defaults
    ) -> TaphapticWatchEventPolicyResult {
        var nextState = state
        nextState.clearExpiredActiveState(at: now)

        if event.id < nextState.lastSeenEventID {
            if nextState.isActive(at: now) {
                return TaphapticWatchEventPolicyResult(state: nextState, action: .noChange)
            }
            return TaphapticWatchEventPolicyResult(state: nextState, action: .showPending)
        }

        if event.id == nextState.activeEventID, nextState.isActive(at: now) {
            return TaphapticWatchEventPolicyResult(state: nextState, action: .noChange)
        }

        guard event.id > nextState.lastSeenEventID else {
            if nextState.isActive(at: now) {
                return TaphapticWatchEventPolicyResult(state: nextState, action: .noChange)
            }
            return TaphapticWatchEventPolicyResult(state: nextState, action: .showPending)
        }

        let age = now.timeIntervalSince(event.createdAt)
        if age > config.staleEventMaxAgeSeconds {
            nextState.lastSeenEventID = event.id
            if nextState.isActive(at: now) {
                return TaphapticWatchEventPolicyResult(state: nextState, action: .noChange)
            }
            return TaphapticWatchEventPolicyResult(state: nextState, action: .showPending)
        }

        nextState.lastSeenEventID = event.id
        nextState.activeEventID = event.id

        let presentation: TaphapticWatchEventPresentation
        switch event.type {
        case .completed:
            let expiresAt = now
                .addingTimeInterval(config.completedAnimationSeconds)
                .addingTimeInterval(config.transientDisplayWindowSeconds)
            nextState.activeEventExpiresAt = expiresAt
            presentation = TaphapticWatchEventPresentation(
                eventID: event.id,
                eventType: event.type,
                detailText: event.resolvedBody,
                animateCompleted: true,
                hapticPlan: .immediate(.completed),
                expiresAt: expiresAt
            )
        case .subagentCompleted:
            let expiresAt = now
                .addingTimeInterval(config.completedAnimationSeconds)
                .addingTimeInterval(config.transientDisplayWindowSeconds)
            nextState.activeEventExpiresAt = expiresAt
            presentation = TaphapticWatchEventPresentation(
                eventID: event.id,
                eventType: event.type,
                detailText: event.resolvedBody,
                animateCompleted: false,
                hapticPlan: .immediate(.subagentCompleted),
                expiresAt: expiresAt
            )
        case .failed:
            let expiresAt = now.addingTimeInterval(config.transientDisplayWindowSeconds)
            nextState.activeEventExpiresAt = expiresAt
            presentation = TaphapticWatchEventPresentation(
                eventID: event.id,
                eventType: event.type,
                detailText: event.resolvedBody,
                animateCompleted: false,
                hapticPlan: .immediate(.failed),
                expiresAt: expiresAt
            )
        case .attention:
            let expiresAt = now.addingTimeInterval(config.transientDisplayWindowSeconds)
            nextState.activeEventExpiresAt = expiresAt
            presentation = TaphapticWatchEventPresentation(
                eventID: event.id,
                eventType: event.type,
                detailText: event.resolvedBody,
                animateCompleted: false,
                hapticPlan: .immediate(.attention),
                expiresAt: expiresAt
            )
        }

        return TaphapticWatchEventPolicyResult(state: nextState, action: .showEvent(presentation))
    }

    static func applyPending(
        state: TaphapticWatchEventPolicyState,
        now: Date
    ) -> TaphapticWatchEventPolicyResult {
        var nextState = state
        nextState.clearExpiredActiveState(at: now)
        if nextState.isActive(at: now) {
            return TaphapticWatchEventPolicyResult(state: nextState, action: .noChange)
        }
        return TaphapticWatchEventPolicyResult(state: nextState, action: .showPending)
    }
}
