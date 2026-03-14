import Foundation

struct AgentWatchWatchEventPolicyConfig: Equatable {
    let completedAnimationSeconds: TimeInterval
    let transientDisplayWindowSeconds: TimeInterval
    let staleEventMaxAgeSeconds: TimeInterval

    static let defaults = AgentWatchWatchEventPolicyConfig(
        completedAnimationSeconds: 1.25,
        transientDisplayWindowSeconds: 10,
        staleEventMaxAgeSeconds: 20
    )
}

struct AgentWatchWatchEventPolicyState: Equatable {
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

enum AgentWatchWatchEventHapticPlan: Equatable {
    case none
    case immediate(AgentWatchEventType)
    case delayed(AgentWatchEventType, TimeInterval)
}

struct AgentWatchWatchEventPresentation: Equatable {
    let eventID: Int64
    let eventType: AgentWatchEventType
    let detailText: String
    let animateCompleted: Bool
    let hapticPlan: AgentWatchWatchEventHapticPlan
    let expiresAt: Date
}

enum AgentWatchWatchEventPolicyAction: Equatable {
    case noChange
    case showPending
    case showEvent(AgentWatchWatchEventPresentation)
}

struct AgentWatchWatchEventPolicyResult: Equatable {
    let state: AgentWatchWatchEventPolicyState
    let action: AgentWatchWatchEventPolicyAction
}

enum AgentWatchWatchEventPolicy {
    static func apply(
        event: AgentWatchEvent,
        state: AgentWatchWatchEventPolicyState,
        now: Date,
        config: AgentWatchWatchEventPolicyConfig = .defaults
    ) -> AgentWatchWatchEventPolicyResult {
        var nextState = state
        nextState.clearExpiredActiveState(at: now)

        if event.id < nextState.lastSeenEventID {
            if nextState.isActive(at: now) {
                return AgentWatchWatchEventPolicyResult(state: nextState, action: .noChange)
            }
            return AgentWatchWatchEventPolicyResult(state: nextState, action: .showPending)
        }

        if event.id == nextState.activeEventID, nextState.isActive(at: now) {
            return AgentWatchWatchEventPolicyResult(state: nextState, action: .noChange)
        }

        guard event.id > nextState.lastSeenEventID else {
            if nextState.isActive(at: now) {
                return AgentWatchWatchEventPolicyResult(state: nextState, action: .noChange)
            }
            return AgentWatchWatchEventPolicyResult(state: nextState, action: .showPending)
        }

        let age = now.timeIntervalSince(event.createdAt)
        if age > config.staleEventMaxAgeSeconds {
            nextState.lastSeenEventID = event.id
            if nextState.isActive(at: now) {
                return AgentWatchWatchEventPolicyResult(state: nextState, action: .noChange)
            }
            return AgentWatchWatchEventPolicyResult(state: nextState, action: .showPending)
        }

        nextState.lastSeenEventID = event.id
        nextState.activeEventID = event.id

        let presentation: AgentWatchWatchEventPresentation
        switch event.type {
        case .completed:
            let expiresAt = now
                .addingTimeInterval(config.completedAnimationSeconds)
                .addingTimeInterval(config.transientDisplayWindowSeconds)
            nextState.activeEventExpiresAt = expiresAt
            presentation = AgentWatchWatchEventPresentation(
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
            presentation = AgentWatchWatchEventPresentation(
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
            presentation = AgentWatchWatchEventPresentation(
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
            presentation = AgentWatchWatchEventPresentation(
                eventID: event.id,
                eventType: event.type,
                detailText: event.resolvedBody,
                animateCompleted: false,
                hapticPlan: .immediate(.attention),
                expiresAt: expiresAt
            )
        }

        return AgentWatchWatchEventPolicyResult(state: nextState, action: .showEvent(presentation))
    }

    static func applyPending(
        state: AgentWatchWatchEventPolicyState,
        now: Date
    ) -> AgentWatchWatchEventPolicyResult {
        var nextState = state
        nextState.clearExpiredActiveState(at: now)
        if nextState.isActive(at: now) {
            return AgentWatchWatchEventPolicyResult(state: nextState, action: .noChange)
        }
        return AgentWatchWatchEventPolicyResult(state: nextState, action: .showPending)
    }
}
