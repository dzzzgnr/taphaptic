import Foundation
import XCTest

final class TaphapticCursorStoreTests: XCTestCase {
    private var defaults: UserDefaults!
    private var suiteName: String!

    override func setUp() {
        super.setUp()
        suiteName = "TaphapticCursorStoreTests.\(UUID().uuidString)"
        defaults = UserDefaults(suiteName: suiteName)
        defaults.removePersistentDomain(forName: suiteName)
    }

    override func tearDown() {
        defaults.removePersistentDomain(forName: suiteName)
        defaults = nil
        suiteName = nil
        super.tearDown()
    }

    func testRoundTripLargeInt64Cursor() {
        let eventID: Int64 = 1_772_712_975_044

        TaphapticCursorStore.writeInt64(
            eventID,
            defaults: defaults,
            stringKey: "cursor.string",
            legacyIntegerKey: "cursor.legacy"
        )

        let loaded = TaphapticCursorStore.readInt64(
            defaults: defaults,
            stringKey: "cursor.string",
            legacyIntegerKey: "cursor.legacy",
            minimumPlausibleValue: TaphapticCursorStore.plausibleUnixMillisecondsLowerBound
        )

        XCTAssertEqual(loaded, eventID)
        XCTAssertEqual(defaults.string(forKey: "cursor.string"), String(eventID))
    }

    func testLegacyImplausibleValueIsIgnored() {
        defaults.set(8_123_456, forKey: "cursor.legacy")

        let loaded = TaphapticCursorStore.readInt64(
            defaults: defaults,
            stringKey: "cursor.string",
            legacyIntegerKey: "cursor.legacy",
            minimumPlausibleValue: TaphapticCursorStore.plausibleUnixMillisecondsLowerBound
        )

        XCTAssertNil(loaded)
        XCTAssertNil(defaults.string(forKey: "cursor.string"))
    }

    func testLegacyPlausibleValueMigratesToString() {
        let legacyValue: Int64 = 1_772_712_975_044
        defaults.set(legacyValue, forKey: "cursor.legacy")

        let loaded = TaphapticCursorStore.readInt64(
            defaults: defaults,
            stringKey: "cursor.string",
            legacyIntegerKey: "cursor.legacy",
            minimumPlausibleValue: TaphapticCursorStore.plausibleUnixMillisecondsLowerBound
        )

        XCTAssertEqual(loaded, legacyValue)
        XCTAssertEqual(defaults.string(forKey: "cursor.string"), String(legacyValue))
    }
}
