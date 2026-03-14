import Foundation

enum AgentWatchCursorStore {
    // Unix milliseconds around Sep 2001; current IDs should be much larger.
    static let plausibleUnixMillisecondsLowerBound: Int64 = 1_000_000_000_000

    static func readInt64(
        defaults: UserDefaults = .standard,
        stringKey: String,
        legacyIntegerKey: String? = nil,
        minimumPlausibleValue: Int64? = nil
    ) -> Int64? {
        if let parsed = parseString(defaults.string(forKey: stringKey)) {
            return parsed
        }

        guard let legacyIntegerKey else {
            return nil
        }
        guard defaults.object(forKey: legacyIntegerKey) != nil else {
            return nil
        }

        let legacyValue = Int64(defaults.integer(forKey: legacyIntegerKey))
        if legacyValue <= 0 {
            return nil
        }
        if let minimumPlausibleValue, legacyValue < minimumPlausibleValue {
            return nil
        }

        defaults.set(String(legacyValue), forKey: stringKey)
        return legacyValue
    }

    static func writeInt64(
        _ value: Int64?,
        defaults: UserDefaults = .standard,
        stringKey: String,
        legacyIntegerKey: String? = nil
    ) {
        guard let value else {
            defaults.removeObject(forKey: stringKey)
            if let legacyIntegerKey {
                defaults.removeObject(forKey: legacyIntegerKey)
            }
            return
        }

        defaults.set(String(value), forKey: stringKey)
        if let legacyIntegerKey {
            defaults.set(value, forKey: legacyIntegerKey)
        }
    }

    private static func parseString(_ raw: String?) -> Int64? {
        let trimmed = (raw ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            return nil
        }
        return Int64(trimmed)
    }
}
