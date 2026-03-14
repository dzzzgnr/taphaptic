import Foundation

enum TaphapticEventType: String, Codable, CaseIterable, Sendable {
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

struct TaphapticEvent: Codable, Equatable, Identifiable, Sendable {
    let id: Int64
    let type: TaphapticEventType
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

struct TaphapticEventsResponse: Decodable, Sendable {
    let events: [TaphapticEvent]
}

struct TaphapticStatusResponse: Decodable, Sendable {
    let current: TaphapticEvent?
    let pushConfigured: Bool?
}

struct TaphapticSyncPayload: Equatable, Sendable {
    static let hasCurrentKey = "taphaptic.hasCurrent"
    static let idKey = "taphaptic.id"
    static let typeKey = "taphaptic.type"
    static let createdAtKey = "taphaptic.createdAt"
    static let sourceKey = "taphaptic.source"
    static let titleKey = "taphaptic.title"
    static let bodyKey = "taphaptic.body"

    let id: Int64
    let type: TaphapticEventType
    let createdAt: String
    let source: String
    let title: String
    let body: String

    init(event: TaphapticEvent) {
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
            let type = TaphapticEventType(rawValue: typeRaw)
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

    func asEvent() -> TaphapticEvent {
        let formatter = ISO8601DateFormatter()
        let parsedDate = formatter.date(from: createdAt) ?? Date()

        return TaphapticEvent(
            id: id,
            type: type,
            createdAt: parsedDate,
            source: source.isEmpty ? nil : source,
            title: title.isEmpty ? nil : title,
            body: body.isEmpty ? nil : body
        )
    }
}

struct TaphapticPhoneSyncState: Equatable, Sendable {
    static let phoneSessionTokenKey = "taphaptic.phoneSessionToken"
    static let apiBaseURLKey = "taphaptic.apiBaseURL"
    static let pollIntervalSecondsKey = "taphaptic.pollIntervalSeconds"

    let payload: TaphapticSyncPayload?
    let phoneSessionToken: String?
    let apiBaseURL: String?
    let pollIntervalSeconds: TimeInterval?

    init(event: TaphapticEvent?, phoneSessionToken: String?, apiBaseURL: String?, pollIntervalSeconds: TimeInterval?) {
        payload = event.map(TaphapticSyncPayload.init(event:))
        self.phoneSessionToken = Self.normalized(phoneSessionToken)
        self.apiBaseURL = Self.normalized(apiBaseURL)
        self.pollIntervalSeconds = Self.normalized(pollIntervalSeconds)
    }

    init(context: [String: Any]) {
        payload = TaphapticSyncPayload(context: context)
        phoneSessionToken = Self.normalized(context[Self.phoneSessionTokenKey] as? String)
        apiBaseURL = Self.normalized(context[Self.apiBaseURLKey] as? String)
        pollIntervalSeconds = Self.normalizedPollInterval(context[Self.pollIntervalSecondsKey])
    }

    var applicationContext: [String: Any] {
        var context = payload?.applicationContext ?? TaphapticSyncPayload.emptyContext
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
