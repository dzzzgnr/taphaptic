import CryptoKit
import Foundation

@MainActor
final class AgentWatchPhoneModel: ObservableObject {
    private enum StorageKeys {
        static let sessionToken = "agentwatchPhoneSessionToken"
        static let pendingPairingToken = "agentwatchPendingPairingToken"
        static let lastSeenEventIDString = "agentwatchPhoneLastSeenEventIDString"
        static let lastSeenEventIDLegacyInteger = "agentwatchPhoneLastSeenEventID"
        static let installationID = "agentwatchPhoneInstallationID"
        static let channelID = "agentwatchPhoneChannelID"
        static let pollIntervalMilliseconds = "agentwatchPhonePollIntervalMilliseconds"
    }

    private enum RequestError: Error {
        case unauthorized
        case conflict
        case gone
        case badServerResponse
    }

    @Published private(set) var currentEvent: AgentWatchEvent?
    @Published private(set) var recentEvents: [AgentWatchEvent] = []
    @Published private(set) var connectionSummary = "Not paired yet. In Claude, run the AgentWatch install command and scan QR with iPhone Camera."
    @Published private(set) var accountSummary = "Waiting for a pairing link."
    @Published private(set) var pushSummary = "Allow notifications to enable haptics."
    @Published private(set) var isRefreshing = false
    @Published private(set) var isPairing = false
    @Published private(set) var isPaired = false
    @Published private(set) var pollIntervalMilliseconds = 1_000

    private let decoder: JSONDecoder
    private let encoder = JSONEncoder()
    private let bridge = AgentWatchPhoneBridge()
    private let notifier = AgentWatchPhoneNotifier()

    private var pollTask: Task<Void, Never>?
    private var notificationObservers: [NSObjectProtocol] = []
    private var hasRegisteredPushDevice = false
    private var backendPushConfigured = false
    private var isActive = false
    private var pollIntervalSeconds: TimeInterval = 1

    init() {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        self.decoder = decoder
        pollIntervalMilliseconds = restoredPollIntervalMilliseconds()
        pollIntervalSeconds = TimeInterval(pollIntervalMilliseconds) / 1_000

        isPaired = storedSessionToken != nil
        if isPaired {
            accountSummary = "Paired on this iPhone."
            connectionSummary = "Open the app to sync status."
        }

        if pendingPairingToken != nil {
            accountSummary = "Pairing link received. Waiting for notification token..."
        }

        if let restoredEvent = AgentWatchPhoneRemoteEventStore.load() {
            currentEvent = restoredEvent
            mergeEvents([restoredEvent])
            lastSeenEventID = max(lastSeenEventID, restoredEvent.id)
            if isPaired {
                connectionSummary = "Connected. Latest: \(restoredEvent.resolvedTitle)"
                pushSyncStateToWatch(event: restoredEvent)
            }
        }

        if storedPushToken != nil {
            pushSummary = isPaired
                ? "Push token ready. Registering with backend when app is active."
                : "Push token ready. Waiting for pairing."
        }

        startObservingPushNotifications()
    }

    func setActive(_ active: Bool) {
        guard active != isActive else {
            return
        }

        isActive = active
        if active {
            registerForPushIfNeeded()
            refreshConfiguration()
            pushSyncStateToWatch()
        } else {
            stopPolling()
        }
    }

    func refreshConfiguration() {
        stopPolling()

        if pendingPairingToken != nil {
            connectionSummary = "Pairing in progress..."
            Task { @MainActor in
                await claimPairingIfPossible()
            }
            return
        }

        guard let _ = storedSessionToken else {
            isPaired = false
            connectionSummary = "Not paired yet. In Claude, run install and scan QR with iPhone Camera."
            accountSummary = "Waiting for a pairing link."
            return
        }

        isPaired = true
        connectionSummary = "Connecting to AgentWatch cloud..."

        Task { @MainActor in
            await registerCurrentDeviceIfPossible()
        }

        if isActive {
            startPollingIfNeeded()
        }
    }

    func manualRefresh() {
        Task { @MainActor in
            await pollOnce()
        }
    }

    func resetPairing() {
        stopPolling()
        clearSessionState(
            accountMessage: "Pairing reset. Scan a new QR link from Claude.",
            connectionMessage: "Not paired yet. In Claude, run install and scan QR with iPhone Camera."
        )
        if storedPushToken != nil {
            pushSummary = "Push token ready. Waiting for pairing."
        }
    }

    func handleIncomingURL(_ url: URL) {
        guard let token = pairingToken(from: url) else {
            return
        }

        pendingPairingToken = token
        accountSummary = "Pairing link accepted. Finalizing connection..."
        connectionSummary = "Pairing in progress..."

        Task { @MainActor in
            await claimPairingIfPossible(force: true)
        }
    }

    func setPollIntervalMilliseconds(_ milliseconds: Int) {
        let clamped = Self.clampPollIntervalMilliseconds(milliseconds)
        guard clamped != pollIntervalMilliseconds else {
            return
        }

        pollIntervalMilliseconds = clamped
        pollIntervalSeconds = TimeInterval(clamped) / 1_000
        UserDefaults.standard.set(clamped, forKey: StorageKeys.pollIntervalMilliseconds)
        pushSyncStateToWatch()

        guard isActive, pendingPairingToken == nil, storedSessionToken != nil else {
            return
        }
        stopPolling()
        startPollingIfNeeded()
    }

    var pollIntervalSummary: String {
        if pollIntervalMilliseconds >= 1_000 {
            let seconds = Double(pollIntervalMilliseconds) / 1_000
            return String(format: "%.3f s", seconds)
        }
        return "\(pollIntervalMilliseconds) ms"
    }

    private func pairingToken(from url: URL) -> String? {
        if let components = URLComponents(url: url, resolvingAgainstBaseURL: false) {
            for item in components.queryItems ?? [] {
                if item.name == "pairingToken" || item.name == "token" {
                    if let token = normalizedPairingToken(item.value ?? "") {
                        return token
                    }
                }
            }
        }

        let host = (url.host ?? "").lowercased()
        if host == "pair.agentwatch.app" || host == "pairagentwatchapp.vercel.app" {
            let parts = url.pathComponents.filter { $0 != "/" && !$0.isEmpty }
            if parts.count >= 2 && parts[0] == "p" {
                return normalizedPairingToken(parts[1])
            }
        }

        if (url.scheme ?? "").lowercased() == "agentwatch" {
            let host = (url.host ?? "").lowercased()
            if host == "p" {
                let parts = url.pathComponents.filter { $0 != "/" && !$0.isEmpty }
                if let first = parts.first {
                    return normalizedPairingToken(first)
                }
            }

            let parts = url.pathComponents.filter { $0 != "/" && !$0.isEmpty }
            if parts.count >= 2 && parts[0] == "p" {
                return normalizedPairingToken(parts[1])
            }
        }

        return nil
    }

    private func normalizedPairingToken(_ raw: String) -> String? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            return nil
        }

        let decoded = trimmed.removingPercentEncoding ?? trimmed
        let scalars = decoded.unicodeScalars.filter { !CharacterSet.whitespacesAndNewlines.contains($0) }
        let normalized = String(String.UnicodeScalarView(scalars)).trimmingCharacters(in: .whitespacesAndNewlines)
        return normalized.isEmpty ? nil : normalized
    }

    private func claimPairingIfPossible(force: Bool = false) async {
        guard let pairingToken = pendingPairingToken else {
            return
        }

        let pushToken = effectivePushToken
        if storedPushToken == nil {
            connectionSummary = "Pairing in progress. Local notification fallback enabled."
            pushSummary = "APNs token unavailable. Using local fallback token for pairing."
            registerForPushIfNeeded()
        }

        if isPairing && !force {
            return
        }

        struct ClaimRequest: Encodable {
            let pairingToken: String
            let phoneInstallationId: String
            let pushToken: String
        }

        struct ClaimResponse: Decodable {
            let phoneSessionToken: String
            let channelId: String
        }

        isPairing = true
        defer { isPairing = false }

        do {
            let response: ClaimResponse = try await post(
                url: apiBaseURL.appendingPathComponent("v1/pairings/claim"),
                body: ClaimRequest(
                    pairingToken: pairingToken,
                    phoneInstallationId: installationID,
                    pushToken: pushToken
                )
            )

            storedSessionToken = response.phoneSessionToken
            storedChannelID = response.channelId
            pendingPairingToken = nil
            isPaired = true
            hasRegisteredPushDevice = false
            pollIntervalSeconds = TimeInterval(pollIntervalMilliseconds) / 1_000
            accountSummary = "Paired successfully. This iPhone now receives Claude status."
            connectionSummary = "Connected. Waiting for latest event..."
            pushSyncStateToWatch()

            await registerCurrentDeviceIfPossible()
            if isActive {
                startPollingIfNeeded()
            }
        } catch RequestError.gone {
            pendingPairingToken = nil
            accountSummary = "Pairing link expired. Re-run install in Claude and scan new QR."
            connectionSummary = "Pairing expired. Scan a fresh QR link."
        } catch RequestError.conflict {
            pendingPairingToken = nil
            accountSummary = "Pairing link already used. Re-run install in Claude."
            connectionSummary = "Pairing link already used."
        } catch {
            accountSummary = "Could not complete pairing. Check network and try scanning again."
            connectionSummary = "Pairing failed. Scan QR again."
        }
    }

    private func startPollingIfNeeded() {
        guard isActive, storedSessionToken != nil, pollTask == nil else {
            return
        }

        pollTask = Task { @MainActor in
            while !Task.isCancelled {
                await pollOnce()
                do {
                    try await Task.sleep(for: .seconds(pollIntervalSeconds))
                } catch {
                    break
                }
            }
        }
    }

    private func stopPolling() {
        pollTask?.cancel()
        pollTask = nil
    }

    private func startObservingPushNotifications() {
        let center = NotificationCenter.default

        notificationObservers.append(
            center.addObserver(
                forName: AgentWatchPhonePushConstants.didRegisterNotification,
                object: nil,
                queue: .main
            ) { [weak self] notification in
                guard
                    let token = notification.userInfo?[AgentWatchPhonePushConstants.deviceTokenUserInfoKey] as? String
                else {
                    return
                }

                Task { @MainActor in
                    self?.handleRegisteredPushToken(token)
                }
            }
        )

        notificationObservers.append(
            center.addObserver(
                forName: AgentWatchPhonePushConstants.didFailNotification,
                object: nil,
                queue: .main
            ) { [weak self] notification in
                let description = notification.userInfo?[AgentWatchPhonePushConstants.errorDescriptionUserInfoKey] as? String

                Task { @MainActor in
                    self?.handlePushRegistrationFailure(description)
                }
            }
        )

        notificationObservers.append(
            center.addObserver(
                forName: AgentWatchPhonePushConstants.didReceiveEventNotification,
                object: nil,
                queue: .main
            ) { [weak self] notification in
                guard
                    let encoded = notification.userInfo?[AgentWatchPhonePushConstants.eventDataUserInfoKey] as? Data,
                    let event = AgentWatchPhoneRemoteEventStore.decode(encoded)
                else {
                    return
                }

                Task { @MainActor in
                    self?.handleRemoteEvent(event)
                }
            }
        )
    }

    private func registerForPushIfNeeded() {
        if storedPushToken == nil {
            pushSummary = "Requesting notification permission and Apple push token."
        }
        notifier.ensureRemoteNotificationsRegistration()
    }

    private func handleRegisteredPushToken(_ token: String) {
        let normalized = token.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !normalized.isEmpty else {
            return
        }

        UserDefaults.standard.set(normalized, forKey: AgentWatchPhonePushConstants.deviceTokenDefaultsKey)
        hasRegisteredPushDevice = false

        if pendingPairingToken != nil {
            pushSummary = "Push token ready. Finishing pairing..."
            Task { @MainActor in
                await claimPairingIfPossible()
            }
            return
        }

        pushSummary = isPaired
            ? "Push token ready. Registering with backend."
            : "Push token ready. Waiting for pairing."

        Task { @MainActor in
            await registerCurrentDeviceIfPossible()
        }
    }

    private func handlePushRegistrationFailure(_ description: String?) {
        let detail = (description ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if detail.isEmpty {
            pushSummary = "Could not register with Apple Push Notification service."
        } else {
            pushSummary = "Push registration failed: \(detail)"
        }
    }

    private func registerCurrentDeviceIfPossible() async {
        guard let sessionToken = storedSessionToken else {
            return
        }
        let pushToken = effectivePushToken

        do {
            try await registerDevice(bearerToken: sessionToken, pushToken: pushToken)
            hasRegisteredPushDevice = true
            if storedPushToken == nil {
                pushSummary = "Using local notification fallback (no Apple push token available)."
            } else {
                pushSummary = backendPushConfigured
                    ? "Push notifications are fully enabled."
                    : "Push token registered. Backend APNs is not enabled yet."
            }
        } catch RequestError.unauthorized {
            clearSessionState(
                accountMessage: "Session expired. Re-scan QR from Claude.",
                connectionMessage: "Session expired. Pair again."
            )
            pushSummary = "Push token ready. Waiting for pairing."
        } catch {
            hasRegisteredPushDevice = false
            pushSummary = "Could not register push token. App will retry."
        }
    }

    private func pollOnce() async {
        guard let sessionToken = storedSessionToken else {
            return
        }

        isRefreshing = true
        defer { isRefreshing = false }

        do {
            let status: AgentWatchStatusResponse = try await fetch(
                url: apiBaseURL.appendingPathComponent("v1/status"),
                bearerToken: sessionToken
            )

            backendPushConfigured = status.pushConfigured == true
            currentEvent = status.current
            var newestID = max(lastSeenEventID, status.current?.id ?? 0)

            var eventsURL = apiBaseURL.appendingPathComponent("v1/events")
            if var components = URLComponents(url: eventsURL, resolvingAgainstBaseURL: false) {
                components.queryItems = [URLQueryItem(name: "since", value: String(lastSeenEventID))]
                if let resolvedURL = components.url {
                    eventsURL = resolvedURL
                }
            }

            let eventsResponse: AgentWatchEventsResponse = try await fetch(
                url: eventsURL,
                bearerToken: sessionToken
            )

            let latestNewEvent = ([status.current] + eventsResponse.events.map(Optional.some))
                .compactMap { $0 }
                .filter { $0.id > lastSeenEventID }
                .max(by: { lhs, rhs in lhs.id < rhs.id })

            mergeEvents(status.current.map { [$0] } ?? [] + eventsResponse.events)
            if let maxReturnedID = recentEvents.first?.id {
                newestID = max(newestID, maxReturnedID)
            }

            if let latestNewEvent, !backendPushConfigured {
                notifier.notify(for: latestNewEvent)
            }

            lastSeenEventID = newestID
            pushSyncStateToWatch(event: status.current)

            if let currentEvent {
                connectionSummary = "Connected. Latest: \(currentEvent.resolvedTitle)"
            } else {
                connectionSummary = "Connected. Waiting for next Claude event."
            }
        } catch RequestError.unauthorized {
            clearSessionState(
                accountMessage: "Session expired. Re-scan QR from Claude.",
                connectionMessage: "Session expired. Pair again."
            )
            pushSummary = storedPushToken == nil
                ? "Allow notifications to enable haptics."
                : "Push token ready. Waiting for pairing."
        } catch {
            connectionSummary = "Could not reach AgentWatch cloud. Check network."
        }
    }

    private func handleRemoteEvent(_ event: AgentWatchEvent) {
        currentEvent = event
        mergeEvents([event])
        lastSeenEventID = max(lastSeenEventID, event.id)
        AgentWatchPhoneRemoteEventStore.save(event)
        pushSyncStateToWatch(event: event)
        backendPushConfigured = true

        if storedPushToken != nil {
            pushSummary = "Push notifications are fully enabled."
        }
        if isPaired {
            connectionSummary = "Connected. Latest: \(event.resolvedTitle)"
        }
    }

    private func mergeEvents(_ incoming: [AgentWatchEvent]) {
        guard !incoming.isEmpty || !recentEvents.isEmpty else {
            return
        }

        var byID: [Int64: AgentWatchEvent] = [:]
        for event in recentEvents {
            byID[event.id] = event
        }
        for event in incoming {
            byID[event.id] = event
        }

        recentEvents = byID.values
            .sorted { lhs, rhs in
                if lhs.id == rhs.id {
                    return lhs.createdAt > rhs.createdAt
                }
                return lhs.id > rhs.id
            }
            .prefix(20)
            .map { $0 }
    }

    private func registerDevice(bearerToken: String, pushToken: String) async throws {
        struct DeviceRegistrationRequest: Encodable {
            let installationId: String
            let platform: String
            let pushToken: String
        }

        var request = URLRequest(url: apiBaseURL.appendingPathComponent("v1/devices"))
        request.httpMethod = "POST"
        request.timeoutInterval = 5
        request.setValue("Bearer \(bearerToken)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try encoder.encode(
            DeviceRegistrationRequest(
                installationId: installationID,
                platform: "ios",
                pushToken: pushToken
            )
        )

        let (_, response) = try await URLSession.shared.data(for: request)
        try validate(response: response)
    }

    private func fetch<Response: Decodable>(url: URL, bearerToken: String) async throws -> Response {
        var request = URLRequest(url: url)
        request.timeoutInterval = 5
        request.setValue("Bearer \(bearerToken)", forHTTPHeaderField: "Authorization")

        let (data, response) = try await URLSession.shared.data(for: request)
        try validate(response: response)
        return try decoder.decode(Response.self, from: data)
    }

    private func post<RequestBody: Encodable, Response: Decodable>(url: URL, body: RequestBody) async throws -> Response {
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.timeoutInterval = 5
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try encoder.encode(body)

        let (data, response) = try await URLSession.shared.data(for: request)
        try validate(response: response)
        return try decoder.decode(Response.self, from: data)
    }

    private func validate(response: URLResponse) throws {
        guard let httpResponse = response as? HTTPURLResponse else {
            throw RequestError.badServerResponse
        }

        switch httpResponse.statusCode {
        case 200:
            return
        case 401:
            throw RequestError.unauthorized
        case 409:
            throw RequestError.conflict
        case 410:
            throw RequestError.gone
        default:
            throw RequestError.badServerResponse
        }
    }

    private func clearSessionState(accountMessage: String, connectionMessage: String) {
        storedSessionToken = nil
        storedChannelID = nil
        pendingPairingToken = nil
        isPaired = false
        hasRegisteredPushDevice = false
        backendPushConfigured = false
        currentEvent = nil
        recentEvents = []
        lastSeenEventID = 0
        AgentWatchPhoneRemoteEventStore.clear()
        pushSyncStateToWatch(event: nil)
        accountSummary = accountMessage
        connectionSummary = connectionMessage
    }

    private func pushSyncStateToWatch(event: AgentWatchEvent? = nil) {
        let resolvedEvent = event ?? currentEvent
        bridge.pushSyncState(
            resolvedEvent,
            phoneSessionToken: storedSessionToken,
            apiBaseURL: apiBaseURL,
            pollIntervalSeconds: pollIntervalSeconds
        )
    }

    private func restoredPollIntervalMilliseconds() -> Int {
        let stored = UserDefaults.standard.integer(forKey: StorageKeys.pollIntervalMilliseconds)
        if UserDefaults.standard.object(forKey: StorageKeys.pollIntervalMilliseconds) == nil {
            return 1_000
        }
        return Self.clampPollIntervalMilliseconds(stored)
    }

    private static func clampPollIntervalMilliseconds(_ value: Int) -> Int {
        min(2_000, max(1, value))
    }

    private var apiBaseURL: URL {
        let fromEnv = (ProcessInfo.processInfo.environment["AGENTWATCH_API_BASE_URL"] ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        if let envURL = URL(string: fromEnv), let host = envURL.host, !host.isEmpty {
            return envURL
        }

        return URL(string: "https://agentwatch-api-production-39a1.up.railway.app")!
    }

    private var storedSessionToken: String? {
        get {
            let rawValue = (UserDefaults.standard.string(forKey: StorageKeys.sessionToken) ?? "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            return rawValue.isEmpty ? nil : rawValue
        }
        set {
            UserDefaults.standard.set(newValue, forKey: StorageKeys.sessionToken)
        }
    }

    private var pendingPairingToken: String? {
        get {
            let rawValue = (UserDefaults.standard.string(forKey: StorageKeys.pendingPairingToken) ?? "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            return rawValue.isEmpty ? nil : rawValue
        }
        set {
            UserDefaults.standard.set(newValue, forKey: StorageKeys.pendingPairingToken)
        }
    }

    private var storedChannelID: String? {
        get {
            let rawValue = (UserDefaults.standard.string(forKey: StorageKeys.channelID) ?? "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            return rawValue.isEmpty ? nil : rawValue
        }
        set {
            UserDefaults.standard.set(newValue, forKey: StorageKeys.channelID)
        }
    }

    private var lastSeenEventID: Int64 {
        get {
            AgentWatchCursorStore.readInt64(
                defaults: .standard,
                stringKey: StorageKeys.lastSeenEventIDString,
                legacyIntegerKey: StorageKeys.lastSeenEventIDLegacyInteger,
                minimumPlausibleValue: AgentWatchCursorStore.plausibleUnixMillisecondsLowerBound
            ) ?? 0
        }
        set {
            AgentWatchCursorStore.writeInt64(
                newValue,
                defaults: .standard,
                stringKey: StorageKeys.lastSeenEventIDString,
                legacyIntegerKey: StorageKeys.lastSeenEventIDLegacyInteger
            )
        }
    }

    private var installationID: String {
        let existing = (UserDefaults.standard.string(forKey: StorageKeys.installationID) ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        if !existing.isEmpty {
            return existing
        }

        let generated = UUID().uuidString.lowercased()
        UserDefaults.standard.set(generated, forKey: StorageKeys.installationID)
        return generated
    }

    private var storedPushToken: String? {
        let rawValue = (UserDefaults.standard.string(forKey: AgentWatchPhonePushConstants.deviceTokenDefaultsKey) ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return rawValue.isEmpty ? nil : rawValue
    }

    private var effectivePushToken: String {
        if let storedPushToken {
            return storedPushToken
        }

        let digest = SHA256.hash(data: Data(installationID.utf8))
        return digest.map { String(format: "%02x", $0) }.joined()
    }
}
