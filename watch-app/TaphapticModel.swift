import AVFoundation
import Foundation
import SwiftUI
import WatchKit

@MainActor
final class TaphapticModel: ObservableObject {
    private static let pairingCodeLength = 4

    enum PairingState: Equatable {
        case notPaired
        case pairing
        case connected
        case failed(String)

        var message: String {
            switch self {
            case .notPaired:
                return "Enter the 4-digit code shown by setup."
            case .pairing:
                return "Pairing..."
            case .connected:
                return "Connected to local Taphaptic server."
            case let .failed(message):
                return message
            }
        }
    }

    enum DisplayState: Equatable {
        case waiting
        case success
        case subagent
        case failure
        case attention

        var title: String {
            switch self {
            case .waiting:
                return "Pending"
            case .success:
                return "YOUR AGENT IS DONE"
            case .subagent:
                return "Claude subagent completed"
            case .failure:
                return "Failed"
            case .attention:
                return "Claude needs your attention"
            }
        }

        var color: Color {
            switch self {
            case .waiting:
                return .gray
            case .success:
                return .green
            case .subagent:
                return .mint
            case .failure:
                return .red
            case .attention:
                return .orange
            }
        }

        var symbolName: String {
            switch self {
            case .waiting:
                return "ellipsis"
            case .success:
                return "checkmark.circle.fill"
            case .subagent:
                return "checkmark.seal.fill"
            case .failure:
                return "xmark.octagon.fill"
            case .attention:
                return "exclamationmark.triangle.fill"
            }
        }
    }

    enum TerminalPresence: Equatable {
        case connected
        case stale
        case waitingFirstEvent
    }

    private enum StorageKeys {
        static let lastWatchEventID = "taphapticLastWatchEventID"
        static let lastTerminalActivityAt = "taphapticLastTerminalActivityAt"
        static let watchSessionToken = "taphapticWatchSessionToken"
        static let watchInstallationID = "taphapticWatchInstallationID"
        static let channelID = "taphapticWatchChannelID"
        static let cloudAPIBaseURL = "taphapticCloudAPIBaseURL"
        static let watchSoundEnabled = "taphapticWatchSoundEnabled"
        static let watchHapticEnabled = "taphapticWatchHapticEnabled"
    }

    private enum RequestError: Error {
        case invalidCode
        case expiredCode
        case alreadyClaimed
        case tooManyAttempts
        case unauthorized
        case badServerResponse
    }

    @Published var pairingCode = ""
    @Published private(set) var pairingState: PairingState = .notPaired
    @Published private(set) var displayState: DisplayState = .waiting
    @Published private(set) var detailText = "Pending"
    @Published private(set) var connectionDetail = "Not paired yet."
    @Published private(set) var pairingHint = "Looking for local server..."
    @Published private(set) var completedPulseToken = 0
    @Published private(set) var watchSoundEnabled = true
    @Published private(set) var watchHapticEnabled = true

    private let decoder: JSONDecoder
    private let speechSynthesizer = AVSpeechSynthesizer()
    private let completedAnimationSeconds: TimeInterval = 1.25
    private let transientDisplayWindowSeconds: TimeInterval = 3
    private let staleEventMaxAgeSeconds: TimeInterval = 20
    private let terminalRecentActivityWindowSeconds: TimeInterval = 10 * 60
    private let completionTitles = [
        "STOP SCROLLING REELS",
        "I SAW YOU OPEN TWITTER",
        "THAT REEL CAN WAIT",
        "YOU'VE SEEN THAT MEME",
        "REDDIT ISN'T GOING ANYWHERE",
        "YES RIGHT NOW",
        "THE CODE IS COOKED",
        "I DID YOUR JOB",
        "COME BACK TO TERMINAL",
        "YOU OWE ME",
        "WHILE YOU WERE GONE",
        "MISS ME?",
        "I KNOW YOU'RE ON TIKTOK",
        "THAT'S ENOUGH SHORTS",
        "THE RABBIT HOLE IS OVER",
        "I WORKED YOU SCROLLED",
        "DONE BEFORE YOUR REEL",
        "FASTER THAN YOUR ATTENTION SPAN",
        "ONE OF US WAS PRODUCTIVE",
        "THAT MEME WAS NOT WORTH IT",
        "NOW PRETEND YOU CODED THIS",
        "YOUR PR IS READY TO STEAL",
        "STOP LURKING ON HN",
        "THAT TWITTER BEEF CAN WAIT",
        "CLOSE THE 47 TABS",
        "NO MORE DOOMSCROLLING",
        "YOU WERE WATCHING COOKING VIDEOS",
        "STOP ADDING TO CART",
        "DONE BEFORE YOUR COFFEE",
        "WORDLE ISN'T WORK",
        "TAB BACK TO TERMINAL",
        "PUT THE PHONE DOWN",
        "YOUR AGENT MISSES YOU",
        "BET YOU FORGOT ABOUT ME",
        "SURPRISE I'M FAST",
        "WERE YOU EVEN WORRIED",
        "STOP WATCHING RUST TUTORIALS",
        "LINKEDIN WON'T SAVE YOU",
        "ENOUGH DISCORD",
        "SNACK BREAK IS OVER",
        "I DID THE HARD PART",
        "COME COLLECT YOUR CODE",
        "STOP REFRESHING FOLLOWERS",
        "THE PODCAST CAN PAUSE",
        "YOU SCROLLED I SHIPPED",
        "HEY LOOK AT ME",
        "CLOSE YOUTUBE ALREADY",
        "THE WIKI HOLE IS OVER",
        "I HOPE YOU HAD FUN",
        "ANYWAY I'M DONE",
    ]
    private var pollTask: Task<Void, Never>?
    private var pendingResetTask: Task<Void, Never>?
    private var activeTransientEventID: Int64?
    private var activeTransientExpiresAt: Date?
    private var lastCompletionTitle: String?
    private var isActive = false
    private var pollIntervalSeconds = 1
    private let serviceDiscovery = TaphapticServiceDiscovery()
    private var discoveryProbeTask: Task<Void, Never>?

    init() {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        self.decoder = decoder
        watchSoundEnabled = loadBool(forKey: StorageKeys.watchSoundEnabled, defaultValue: true)
        watchHapticEnabled = loadBool(forKey: StorageKeys.watchHapticEnabled, defaultValue: true)
        ensureWatchInstallationID()
        configureServiceDiscovery()
        if let configuredBaseURL = configuredBaseURLFromEnvironment() {
            cloudBaseURL = configuredBaseURL
        }
        refreshConfiguration()
    }

    func setActive(_ active: Bool) {
        isActive = active
        if !active {
            stopPolling()
            stopDiscovery()
            return
        }

        startDiscovery()
        if isPaired {
            startPollingIfPossible()
        }
    }

    func refreshConfiguration() {
        if isPaired {
            pairingState = .connected
            connectionDetail = "Connected"
            showPendingStatusIfNeeded()
            startPollingIfPossible()
            return
        }

        pairingState = .notPaired
        connectionDetail = cloudBaseURL == nil ? "Waiting for local server..." : "Server found. Enter code."
        pairingHint = cloudBaseURL == nil ? "Run setup on your Mac" : "Enter 4-digit code"
        displayState = .waiting
        detailText = "Enter 4-digit code"
    }

    var isPairingInProgress: Bool {
        isPairing
    }

    var canSubmitPairingCode: Bool {
        pairingCode.count == Self.pairingCodeLength && !isPairing
    }

    func pairingDigit(at index: Int) -> String {
        guard index >= 0, index < pairingCode.count else {
            return ""
        }
        let stringIndex = pairingCode.index(pairingCode.startIndex, offsetBy: index)
        return String(pairingCode[stringIndex])
    }

    func appendPairingDigit(_ digit: String) {
        guard !isPairing else {
            return
        }

        let normalized = digit.filter { $0.isNumber }
        guard normalized.count == 1 else {
            return
        }

        if case .failed = pairingState {
            pairingCode = ""
            clearPairingFailureStateForInput()
        }

        guard pairingCode.count < Self.pairingCodeLength else {
            return
        }

        pairingCode.append(normalized)
    }

    func removeLastPairingDigit() {
        guard !isPairing else {
            return
        }

        if case .failed = pairingState {
            pairingCode = ""
            clearPairingFailureStateForInput()
            return
        }

        guard !pairingCode.isEmpty else {
            return
        }

        pairingCode.removeLast()
    }

    private func clearPairingFailureStateForInput() {
        guard case .failed = pairingState else {
            return
        }
        pairingState = .notPaired
        if !isPaired {
            connectionDetail = "Not paired yet."
        }
    }

    func setWatchSoundEnabled(_ enabled: Bool) {
        watchSoundEnabled = enabled
        if !enabled {
            speechSynthesizer.stopSpeaking(at: .immediate)
        }
        UserDefaults.standard.set(enabled, forKey: StorageKeys.watchSoundEnabled)
    }

    func setWatchHapticEnabled(_ enabled: Bool) {
        watchHapticEnabled = enabled
        UserDefaults.standard.set(enabled, forKey: StorageKeys.watchHapticEnabled)
    }

    func submitPairingCode() {
        guard !isPairing else {
            return
        }

        let normalized = normalizedCode(pairingCode)
        guard normalized.count == Self.pairingCodeLength else {
            pairingState = .failed("Code must be 4 digits.")
            return
        }

        guard let baseURL = cloudBaseURL else {
            pairingState = .failed("Waiting for local server. Run setup on Mac.")
            return
        }

        pairingState = .pairing
        connectionDetail = "Claiming code..."

        struct ClaimRequest: Encodable {
            let code: String
            let watchInstallationId: String
            let pushToken: String
        }

        struct ClaimResponse: Decodable {
            let watchSessionToken: String
            let channelId: String
            let pollIntervalSeconds: Int
        }

        Task { @MainActor in
            do {
                let isHealthy = await isHealthyBackend(baseURL)
                guard isHealthy else {
                    cloudBaseURL = nil
                    pairingState = .failed("Waiting for local server. Run setup on Mac.")
                    connectionDetail = "Waiting for local server..."
                    pairingHint = "Run setup on your Mac"
                    return
                }

                let response: ClaimResponse = try await post(
                    url: baseURL.appendingPathComponent("v1/watch/pairings/claim"),
                    body: ClaimRequest(
                        code: normalized,
                        watchInstallationId: watchInstallationID,
                        pushToken: fallbackPushToken()
                    )
                )

                watchSessionToken = response.watchSessionToken
                channelID = response.channelId
                pollIntervalSeconds = max(1, response.pollIntervalSeconds)
                pairingCode = ""
                pairingState = .connected
                connectionDetail = "Connected"
                showPendingStatus("Connected. Pending.")
                startPollingIfPossible()
            } catch RequestError.invalidCode {
                pairingState = .failed("Invalid code. Check and retry.")
            } catch RequestError.expiredCode {
                pairingState = .failed("Code expired. Generate a new code in Claude.")
            } catch RequestError.alreadyClaimed {
                pairingState = .failed("Code already used. Generate a new one.")
            } catch RequestError.tooManyAttempts {
                pairingState = .failed("Too many attempts. Generate a new code.")
            } catch {
                pairingState = .failed("Pairing failed. Check local network and retry.")
            }
        }
    }

    func resetPairing() {
        stopPolling()
        watchSessionToken = nil
        channelID = nil
        lastWatchEventID = 0
        lastTerminalActivityAt = nil
        pairingCode = ""
        pairingState = .notPaired
        connectionDetail = cloudBaseURL == nil ? "Waiting for local server..." : "Server found. Enter code."
        pairingHint = cloudBaseURL == nil ? "Run setup on your Mac" : "Enter 4-digit code"
        speechSynthesizer.stopSpeaking(at: .immediate)
        showPendingStatus("Enter 4-digit code")
    }

    private func applyEvent(_ event: TaphapticEvent) {
        let now = Date()
        let maxAgeDate = now.addingTimeInterval(-staleEventMaxAgeSeconds)
        if event.id <= lastWatchEventID {
            return
        }

        markTerminalActivity(at: event.createdAt)

        if event.createdAt < maxAgeDate {
            lastWatchEventID = event.id
            if !isTransientActive(now: now) {
                showPendingStatusIfNeeded()
            }
            return
        }

        pendingResetTask?.cancel()
        pendingResetTask = nil

        activeTransientEventID = event.id
        lastWatchEventID = event.id
        displayState = displayState(for: event.type)
        detailText = normalizedDetailText(for: event)
        connectionDetail = "Connected"

        let holdStartAt = now.addingTimeInterval(completedAnimationSeconds)
        let expiresAt = holdStartAt.addingTimeInterval(transientDisplayWindowSeconds)
        activeTransientExpiresAt = expiresAt

        if event.type == .completed {
            completedPulseToken += 1
        }
        playAlertFeedback(for: event.type)

        schedulePendingReset(forEventID: event.id, at: expiresAt)
    }

    private func startPollingIfPossible() {
        guard isActive, watchSessionToken != nil, cloudBaseURL != nil, pollTask == nil else {
            return
        }

        pollTask = Task { @MainActor in
            while !Task.isCancelled {
                await pollCloudStatusOnce()
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

    private func pollCloudStatusOnce() async {
        guard let sessionToken = watchSessionToken, let baseURL = cloudBaseURL else {
            return
        }

        do {
            var eventsURL = baseURL.appendingPathComponent("v1/events")
            if var components = URLComponents(url: eventsURL, resolvingAgainstBaseURL: false) {
                components.queryItems = [URLQueryItem(name: "since", value: String(lastWatchEventID))]
                if let resolvedURL = components.url {
                    eventsURL = resolvedURL
                }
            }

            let response: TaphapticEventsResponse = try await fetchEvents(
                url: eventsURL,
                bearerToken: sessionToken
            )

            pairingState = .connected
            connectionDetail = "Connected"

            if response.events.isEmpty {
                if !isTransientActive(now: Date()) {
                    showPendingStatusIfNeeded()
                }
            } else {
                for event in response.events {
                    applyEvent(event)
                }
            }
        } catch RequestError.unauthorized {
            watchSessionToken = nil
            channelID = nil
            pairingState = .failed("Session expired. Enter a new code.")
            connectionDetail = "Session expired"
            showPendingStatus("Session expired. Re-enter code.")
            stopPolling()
        } catch {
            if pairingState == .connected {
                connectionDetail = "Connection issue. Retrying..."
            }
        }
    }

    private func post<Response: Decodable, Body: Encodable>(url: URL, body: Body) async throws -> Response {
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.timeoutInterval = 10
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)

        let (data, response) = try await URLSession.shared.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw RequestError.badServerResponse
        }

        switch httpResponse.statusCode {
        case 200:
            return try decoder.decode(Response.self, from: data)
        case 400:
            throw RequestError.invalidCode
        case 404:
            throw RequestError.invalidCode
        case 409:
            throw RequestError.alreadyClaimed
        case 410:
            throw RequestError.expiredCode
        case 429:
            throw RequestError.tooManyAttempts
        default:
            throw RequestError.badServerResponse
        }
    }

    private func fetchEvents(url: URL, bearerToken: String) async throws -> TaphapticEventsResponse {
        var request = URLRequest(url: url)
        request.timeoutInterval = 5
        request.setValue("Bearer \(bearerToken)", forHTTPHeaderField: "Authorization")

        let (data, response) = try await URLSession.shared.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw RequestError.badServerResponse
        }

        switch httpResponse.statusCode {
        case 200:
            return try decoder.decode(TaphapticEventsResponse.self, from: data)
        case 401:
            throw RequestError.unauthorized
        default:
            throw RequestError.badServerResponse
        }
    }

    private func displayState(for eventType: TaphapticEventType) -> DisplayState {
        switch eventType {
        case .completed:
            return .success
        case .subagentCompleted:
            return .subagent
        case .failed:
            return .failure
        case .attention:
            return .attention
        }
    }

    private func playAlertFeedback(for eventType: TaphapticEventType) {
        if eventType == .completed {
            playCompletedFeedback()
            return
        }

        if watchHapticEnabled {
            playHaptic(for: eventType)
        }
        if watchSoundEnabled {
            playSound(for: eventType)
        }
    }

    private func playCompletedFeedback() {
        switch (watchSoundEnabled, watchHapticEnabled) {
        case (true, true):
            // Native success cue includes both haptic and completion chime.
            WKInterfaceDevice.current().play(.success)
        case (true, false):
            playSound(for: .completed)
        case (false, true):
            speechSynthesizer.stopSpeaking(at: .immediate)
            // Prefer a haptic with minimal or no tonal accompaniment.
            WKInterfaceDevice.current().play(.click)
        case (false, false):
            speechSynthesizer.stopSpeaking(at: .immediate)
            break
        }
    }

    private func playHaptic(for eventType: TaphapticEventType) {
        let hapticType: WKHapticType
        switch eventType {
        case .completed:
            hapticType = .directionUp
        case .subagentCompleted:
            hapticType = .directionUp
        case .failed:
            hapticType = .failure
        case .attention:
            hapticType = .notification
        }

        WKInterfaceDevice.current().play(hapticType)
    }

    private func playSound(for eventType: TaphapticEventType) {
        let utterance = AVSpeechUtterance(string: spokenAlertText(for: eventType))
        utterance.rate = 0.46
        utterance.pitchMultiplier = pitch(for: eventType)
        utterance.volume = 1.0
        speechSynthesizer.stopSpeaking(at: .immediate)
        speechSynthesizer.speak(utterance)
    }

    private func spokenAlertText(for eventType: TaphapticEventType) -> String {
        switch eventType {
        case .completed:
            return "Agent completed a task."
        case .subagentCompleted:
            return "Claude subagent completed."
        case .failed:
            return "Claude failed."
        case .attention:
            return "Claude needs your attention."
        }
    }

    private func normalizedDetailText(for event: TaphapticEvent) -> String {
        if event.type == .completed {
            return randomCompletionTitle()
        }
        return event.resolvedBody
    }

    private func randomCompletionTitle() -> String {
        guard !completionTitles.isEmpty else {
            return "YOUR AGENT IS DONE"
        }

        if completionTitles.count == 1 {
            let onlyTitle = completionTitles[0]
            lastCompletionTitle = onlyTitle
            return onlyTitle
        }

        let candidates = completionTitles.filter { $0 != lastCompletionTitle }
        let selected = (candidates.randomElement() ?? completionTitles.randomElement()) ?? "YOUR AGENT IS DONE"
        lastCompletionTitle = selected
        return selected
    }

    private func pitch(for eventType: TaphapticEventType) -> Float {
        switch eventType {
        case .completed:
            return 1.1
        case .subagentCompleted:
            return 1.2
        case .failed:
            return 0.88
        case .attention:
            return 1.0
        }
    }

    private func showPendingStatus(_ detail: String) {
        pendingResetTask?.cancel()
        pendingResetTask = nil
        activeTransientEventID = nil
        activeTransientExpiresAt = nil
        displayState = .waiting
        detailText = detail
    }

    private func showPendingStatusIfNeeded() {
        showPendingStatus("Pending")
    }

    private func schedulePendingReset(forEventID eventID: Int64, at expiresAt: Date) {
        pendingResetTask?.cancel()

        let delay = max(0, expiresAt.timeIntervalSinceNow)
        pendingResetTask = Task { @MainActor [weak self] in
            do {
                try await Task.sleep(for: .seconds(delay))
            } catch {
                return
            }

            guard let self else {
                return
            }

            guard self.activeTransientEventID == eventID else {
                self.pendingResetTask = nil
                return
            }

            self.showPendingStatusIfNeeded()
            self.pendingResetTask = nil
        }
    }

    private func isTransientActive(now: Date) -> Bool {
        guard let activeTransientEventID, activeTransientEventID > 0 else {
            return false
        }
        guard let activeTransientExpiresAt, activeTransientExpiresAt > now else {
            return false
        }
        return true
    }

    private var lastWatchEventID: Int64 {
        get {
            Int64(UserDefaults.standard.string(forKey: StorageKeys.lastWatchEventID) ?? "") ?? 0
        }
        set {
            UserDefaults.standard.set(String(newValue), forKey: StorageKeys.lastWatchEventID)
        }
    }

    private var lastTerminalActivityAt: Date? {
        get {
            guard let value = UserDefaults.standard.object(forKey: StorageKeys.lastTerminalActivityAt) else {
                return nil
            }
            if let number = value as? NSNumber {
                return Date(timeIntervalSince1970: number.doubleValue)
            }
            return nil
        }
        set {
            guard let newValue else {
                UserDefaults.standard.removeObject(forKey: StorageKeys.lastTerminalActivityAt)
                return
            }
            UserDefaults.standard.set(newValue.timeIntervalSince1970, forKey: StorageKeys.lastTerminalActivityAt)
        }
    }

    private var watchSessionToken: String? {
        get {
            let value = (UserDefaults.standard.string(forKey: StorageKeys.watchSessionToken) ?? "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            return value.isEmpty ? nil : value
        }
        set {
            UserDefaults.standard.set(newValue, forKey: StorageKeys.watchSessionToken)
        }
    }

    private var cloudBaseURL: URL? {
        get {
            let value = (UserDefaults.standard.string(forKey: StorageKeys.cloudAPIBaseURL) ?? "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            guard !value.isEmpty else {
                return nil
            }
            return URL(string: value)
        }
        set {
            UserDefaults.standard.set(newValue?.absoluteString, forKey: StorageKeys.cloudAPIBaseURL)
        }
    }

    private var channelID: String? {
        get {
            let value = (UserDefaults.standard.string(forKey: StorageKeys.channelID) ?? "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            return value.isEmpty ? nil : value
        }
        set {
            UserDefaults.standard.set(newValue, forKey: StorageKeys.channelID)
        }
    }

    private var isPairing: Bool {
        if case .pairing = pairingState {
            return true
        }
        return false
    }

    var isPaired: Bool {
        watchSessionToken != nil
    }

    var terminalPresence: TerminalPresence {
        if let lastTerminalActivityAt {
            let age = Date().timeIntervalSince(lastTerminalActivityAt)
            if age <= terminalRecentActivityWindowSeconds {
                return .connected
            }
            return .stale
        }

        if lastWatchEventID > 0 {
            return .stale
        }

        return .waitingFirstEvent
    }

    private var watchInstallationID: String {
        let value = (UserDefaults.standard.string(forKey: StorageKeys.watchInstallationID) ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        if !value.isEmpty {
            return value
        }

        let generated = "watch-" + UUID().uuidString.lowercased()
        UserDefaults.standard.set(generated, forKey: StorageKeys.watchInstallationID)
        return generated
    }

    private func ensureWatchInstallationID() {
        _ = watchInstallationID
    }

    private func configureServiceDiscovery() {
        serviceDiscovery.onAvailabilityChanged = { [weak self] available in
            Task { @MainActor in
                guard let self else {
                    return
                }
                if self.isPaired || self.cloudBaseURL != nil {
                    return
                }
                self.connectionDetail = available
                    ? "Found local server. Enter code."
                    : "Waiting for local server..."
                self.pairingHint = available
                    ? "Enter 4-digit code"
                    : "Run setup on your Mac"
            }
        }

        serviceDiscovery.onURLResolved = { [weak self] url in
            Task { @MainActor in
                await self?.considerResolvedURL(url)
            }
        }
    }

    private func startDiscovery() {
        serviceDiscovery.start()
    }

    private func stopDiscovery() {
        serviceDiscovery.stop()
        discoveryProbeTask?.cancel()
        discoveryProbeTask = nil
    }

    private func considerResolvedURL(_ url: URL) async {
        if cloudBaseURL == url {
            return
        }

        discoveryProbeTask?.cancel()
        discoveryProbeTask = Task { [weak self] in
            guard let self else {
                return
            }
            let isHealthy = await self.isHealthyBackend(url)
            await MainActor.run {
                guard isHealthy else {
                    return
                }
                self.cloudBaseURL = url
                if self.isPaired {
                    self.connectionDetail = "Connected"
                    self.startPollingIfPossible()
                } else {
                    self.connectionDetail = "Server found. Enter code."
                    self.pairingHint = "Enter 4-digit code"
                }
            }
        }
    }

    private func isHealthyBackend(_ baseURL: URL) async -> Bool {
        var healthURL = baseURL
        healthURL.append(path: "healthz")

        var request = URLRequest(url: healthURL)
        request.httpMethod = "GET"
        request.timeoutInterval = 1.5

        do {
            let (_, response) = try await URLSession.shared.data(for: request)
            guard let httpResponse = response as? HTTPURLResponse else {
                return false
            }
            return httpResponse.statusCode == 204
        } catch {
            return false
        }
    }

    private func configuredBaseURLFromEnvironment() -> URL? {
        let fromTaphapticEnvironment = (ProcessInfo.processInfo.environment["TAPHAPTIC_API_BASE_URL"] ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        if !fromTaphapticEnvironment.isEmpty, let url = URL(string: fromTaphapticEnvironment) {
            return url
        }
        return nil
    }

    private func fallbackPushToken() -> String {
        let data = Data(watchInstallationID.utf8)
        let digest = data.reduce(into: UInt64(1469598103934665603)) { partial, byte in
            partial ^= UInt64(byte)
            partial &*= 1099511628211
        }
        return String(format: "%016llx%016llx", digest, digest ^ 0x9e3779b97f4a7c15)
    }

    private func normalizedCode(_ raw: String) -> String {
        raw.filter { $0.isNumber }
    }

    private func markTerminalActivity(at createdAt: Date) {
        if let currentLast = lastTerminalActivityAt, currentLast >= createdAt {
            return
        }
        lastTerminalActivityAt = createdAt
    }

    private func loadBool(forKey key: String, defaultValue: Bool) -> Bool {
        if UserDefaults.standard.object(forKey: key) == nil {
            return defaultValue
        }
        return UserDefaults.standard.bool(forKey: key)
    }
}
