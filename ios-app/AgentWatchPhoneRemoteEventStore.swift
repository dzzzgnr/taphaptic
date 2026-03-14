import Foundation

enum AgentWatchPhoneRemoteEventStore {
    private static let storageKey = "agentwatchLastRemoteEvent"

    static func save(_ event: AgentWatchEvent) {
        guard let encoded = encode(event) else {
            return
        }

        UserDefaults.standard.set(encoded, forKey: storageKey)
    }

    static func load() -> AgentWatchEvent? {
        guard let data = UserDefaults.standard.data(forKey: storageKey) else {
            return nil
        }

        return decode(data)
    }

    static func clear() {
        UserDefaults.standard.removeObject(forKey: storageKey)
    }

    static func encode(_ event: AgentWatchEvent) -> Data? {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return try? encoder.encode(event)
    }

    static func decode(_ data: Data) -> AgentWatchEvent? {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try? decoder.decode(AgentWatchEvent.self, from: data)
    }
}

enum AgentWatchPhoneRemoteEventParser {
    static func parse(userInfo: [AnyHashable: Any]) -> AgentWatchEvent? {
        guard let payload = userInfo["agentwatch"] as? [String: Any] else {
            return nil
        }

        guard
            let typeRaw = payload["type"] as? String,
            let eventType = AgentWatchEventType(rawValue: typeRaw),
            let eventID = int64(from: payload["id"])
        else {
            return nil
        }

        let createdAtRaw = (payload["createdAt"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let formatter = ISO8601DateFormatter()
        let createdAt = formatter.date(from: createdAtRaw) ?? Date()
        let source = (payload["source"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines)
        let title = (payload["title"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines)
        let body = (payload["body"] as? String)?.trimmingCharacters(in: .whitespacesAndNewlines)

        return AgentWatchEvent(
            id: eventID,
            type: eventType,
            createdAt: createdAt,
            source: source?.isEmpty == true ? nil : source,
            title: title?.isEmpty == true ? nil : title,
            body: body?.isEmpty == true ? nil : body
        )
    }

    private static func int64(from value: Any?) -> Int64? {
        switch value {
        case let number as NSNumber:
            return number.int64Value
        case let value as Int64:
            return value
        case let value as Int:
            return Int64(value)
        case let value as String:
            return Int64(value.trimmingCharacters(in: .whitespacesAndNewlines))
        default:
            return nil
        }
    }
}
