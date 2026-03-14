import Foundation

enum AgentWatchEventType: String, Codable, CaseIterable, Sendable {
    case completed
    case subagentCompleted = "subagent_completed"
    case failed
    case attention

    var fallbackTitle: String {
        switch self {
        case .completed:
            return "Claude completed"
        case .subagentCompleted:
            return "Claude subagent completed"
        case .failed:
            return "Failed"
        case .attention:
            return "Claude needs your attention"
        }
    }

    var fallbackBody: String {
        switch self {
        case .completed:
            return "AGENT COMPLETED A TASK"
        case .subagentCompleted:
            return "Claude Code subagent finished background work."
        case .failed:
            return "Claude Code reported a failure."
        case .attention:
            return "Claude Code needs your attention."
        }
    }

    var systemImageName: String {
        switch self {
        case .completed:
            return "checkmark.circle.fill"
        case .subagentCompleted:
            return "checkmark.seal.fill"
        case .failed:
            return "xmark.circle.fill"
        case .attention:
            return "exclamationmark.triangle.fill"
        }
    }
}

struct AgentWatchEvent: Codable, Equatable, Identifiable, Sendable {
    let id: Int64
    let type: AgentWatchEventType
    let createdAt: Date
    let source: String?
    let title: String?
    let body: String?

    var resolvedTitle: String {
        let trimmedTitle = title?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmedTitle.isEmpty ? type.fallbackTitle : trimmedTitle
    }

    var resolvedBody: String {
        let trimmedBody = body?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        return trimmedBody.isEmpty ? type.fallbackBody : trimmedBody
    }
}

struct AgentWatchEventsResponse: Decodable, Sendable {
    let events: [AgentWatchEvent]
}

struct AgentWatchStatusResponse: Decodable, Sendable {
    let current: AgentWatchEvent?
    let pushConfigured: Bool?
}

struct AgentWatchSyncPayload: Equatable, Sendable {
    static let hasCurrentKey = "agentwatch.hasCurrent"
    static let idKey = "agentwatch.id"
    static let typeKey = "agentwatch.type"
    static let createdAtKey = "agentwatch.createdAt"
    static let sourceKey = "agentwatch.source"
    static let titleKey = "agentwatch.title"
    static let bodyKey = "agentwatch.body"

    let id: Int64
    let type: AgentWatchEventType
    let createdAt: String
    let source: String
    let title: String
    let body: String

    init(event: AgentWatchEvent) {
        let formatter = ISO8601DateFormatter()

        id = event.id
        type = event.type
        createdAt = formatter.string(from: event.createdAt)
        source = (event.source ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        title = event.resolvedTitle
        body = event.resolvedBody
    }

    init?(context: [String: Any]) {
        if let hasCurrent = context[Self.hasCurrentKey] as? Bool, !hasCurrent {
            return nil
        }

        guard
            let typeRaw = context[Self.typeKey] as? String,
            let type = AgentWatchEventType(rawValue: typeRaw)
        else {
            return nil
        }

        if let number = context[Self.idKey] as? NSNumber {
            id = number.int64Value
        } else if let value = context[Self.idKey] as? Int64 {
            id = value
        } else if let value = context[Self.idKey] as? Int {
            id = Int64(value)
        } else {
            return nil
        }

        self.type = type
        createdAt = context[Self.createdAtKey] as? String ?? ""
        source = context[Self.sourceKey] as? String ?? ""
        title = context[Self.titleKey] as? String ?? type.fallbackTitle
        body = context[Self.bodyKey] as? String ?? type.fallbackBody
    }

    var applicationContext: [String: Any] {
        [
            Self.hasCurrentKey: true,
            Self.idKey: NSNumber(value: id),
            Self.typeKey: type.rawValue,
            Self.createdAtKey: createdAt,
            Self.sourceKey: source,
            Self.titleKey: title,
            Self.bodyKey: body,
        ]
    }

    static var emptyContext: [String: Any] {
        [hasCurrentKey: false]
    }

    func asEvent() -> AgentWatchEvent {
        let formatter = ISO8601DateFormatter()
        let parsedDate = formatter.date(from: createdAt) ?? Date()

        return AgentWatchEvent(
            id: id,
            type: type,
            createdAt: parsedDate,
            source: source.isEmpty ? nil : source,
            title: title.isEmpty ? nil : title,
            body: body.isEmpty ? nil : body
        )
    }
}

struct AgentWatchPhoneSyncState: Equatable, Sendable {
    static let phoneSessionTokenKey = "agentwatch.phoneSessionToken"
    static let apiBaseURLKey = "agentwatch.apiBaseURL"
    static let pollIntervalSecondsKey = "agentwatch.pollIntervalSeconds"

    let payload: AgentWatchSyncPayload?
    let phoneSessionToken: String?
    let apiBaseURL: String?
    let pollIntervalSeconds: TimeInterval?

    init(event: AgentWatchEvent?, phoneSessionToken: String?, apiBaseURL: String?, pollIntervalSeconds: TimeInterval?) {
        payload = event.map(AgentWatchSyncPayload.init(event:))
        self.phoneSessionToken = Self.normalized(phoneSessionToken)
        self.apiBaseURL = Self.normalized(apiBaseURL)
        self.pollIntervalSeconds = Self.normalized(pollIntervalSeconds)
    }

    init(context: [String: Any]) {
        payload = AgentWatchSyncPayload(context: context)
        phoneSessionToken = Self.normalized(context[Self.phoneSessionTokenKey] as? String)
        apiBaseURL = Self.normalized(context[Self.apiBaseURLKey] as? String)
        pollIntervalSeconds = Self.normalizedPollInterval(context[Self.pollIntervalSecondsKey])
    }

    var applicationContext: [String: Any] {
        var context = payload?.applicationContext ?? AgentWatchSyncPayload.emptyContext
        if let phoneSessionToken {
            context[Self.phoneSessionTokenKey] = phoneSessionToken
        }
        if let apiBaseURL {
            context[Self.apiBaseURLKey] = apiBaseURL
        }
        if let pollIntervalSeconds {
            context[Self.pollIntervalSecondsKey] = pollIntervalSeconds
        }
        return context
    }

    private static func normalized(_ raw: String?) -> String? {
        let trimmed = (raw ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.isEmpty ? nil : trimmed
    }

    private static func normalized(_ raw: TimeInterval?) -> TimeInterval? {
        guard let raw else {
            return nil
        }
        let clamped = min(2, max(0.001, raw))
        return clamped.isFinite ? clamped : nil
    }

    private static func normalizedPollInterval(_ raw: Any?) -> TimeInterval? {
        if let number = raw as? NSNumber {
            return normalized(number.doubleValue)
        }
        if let value = raw as? TimeInterval {
            return normalized(value)
        }
        if let value = raw as? String {
            return normalized(TimeInterval(value))
        }
        return nil
    }
}
