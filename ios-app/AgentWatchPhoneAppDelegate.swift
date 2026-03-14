import Foundation
import UIKit

enum AgentWatchPhonePushConstants {
    static let deviceTokenDefaultsKey = "agentwatchAPNSDeviceToken"
    static let didRegisterNotification = Notification.Name("AgentWatchDidRegisterRemoteNotifications")
    static let didFailNotification = Notification.Name("AgentWatchDidFailRemoteNotifications")
    static let didReceiveEventNotification = Notification.Name("AgentWatchDidReceiveRemoteEvent")
    static let deviceTokenUserInfoKey = "deviceToken"
    static let errorDescriptionUserInfoKey = "errorDescription"
    static let eventDataUserInfoKey = "eventData"
}

@MainActor
final class AgentWatchPhoneAppDelegate: NSObject, UIApplicationDelegate {
    private enum StorageKeys {
        static let sessionToken = "agentwatchPhoneSessionToken"
        static let pollIntervalMilliseconds = "agentwatchPhonePollIntervalMilliseconds"
    }

    private enum RequestError: Error {
        case badServerResponse
        case unauthorized
    }

    private let bridge = AgentWatchPhoneBridge()

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        application.setMinimumBackgroundFetchInterval(UIApplication.backgroundFetchIntervalMinimum)
        return true
    }

    func application(
        _ application: UIApplication,
        didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
    ) {
        let token = deviceToken.map { String(format: "%02x", $0) }.joined()
        UserDefaults.standard.set(token, forKey: AgentWatchPhonePushConstants.deviceTokenDefaultsKey)

        NotificationCenter.default.post(
            name: AgentWatchPhonePushConstants.didRegisterNotification,
            object: nil,
            userInfo: [AgentWatchPhonePushConstants.deviceTokenUserInfoKey: token]
        )
    }

    func application(
        _ application: UIApplication,
        didFailToRegisterForRemoteNotificationsWithError error: any Error
    ) {
        NotificationCenter.default.post(
            name: AgentWatchPhonePushConstants.didFailNotification,
            object: nil,
            userInfo: [AgentWatchPhonePushConstants.errorDescriptionUserInfoKey: error.localizedDescription]
        )
    }

    func application(
        _ application: UIApplication,
        didReceiveRemoteNotification userInfo: [AnyHashable: Any],
        fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
    ) {
        guard let event = AgentWatchPhoneRemoteEventParser.parse(userInfo: userInfo) else {
            completionHandler(.noData)
            return
        }

        AgentWatchPhoneRemoteEventStore.save(event)
        if let encoded = AgentWatchPhoneRemoteEventStore.encode(event) {
            NotificationCenter.default.post(
                name: AgentWatchPhonePushConstants.didReceiveEventNotification,
                object: nil,
                userInfo: [AgentWatchPhonePushConstants.eventDataUserInfoKey: encoded]
            )
        }

        bridge.pushSyncState(
            event,
            phoneSessionToken: storedSessionToken,
            apiBaseURL: apiBaseURL,
            pollIntervalSeconds: configuredPollIntervalSeconds
        )
        completionHandler(.newData)
    }

    func application(
        _ application: UIApplication,
        performFetchWithCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
    ) {
        guard let sessionToken = storedSessionToken else {
            completionHandler(.noData)
            return
        }

        Task {
            do {
                let fetchedEvent = try await fetchLatestStatusEvent(sessionToken: sessionToken)
                guard let fetchedEvent else {
                    completionHandler(.noData)
                    return
                }

                if let current = AgentWatchPhoneRemoteEventStore.load(), current.id >= fetchedEvent.id {
                    completionHandler(.noData)
                    return
                }

                AgentWatchPhoneRemoteEventStore.save(fetchedEvent)
                if let encoded = AgentWatchPhoneRemoteEventStore.encode(fetchedEvent) {
                    NotificationCenter.default.post(
                        name: AgentWatchPhonePushConstants.didReceiveEventNotification,
                        object: nil,
                        userInfo: [AgentWatchPhonePushConstants.eventDataUserInfoKey: encoded]
                    )
                }

                bridge.pushSyncState(
                    fetchedEvent,
                    phoneSessionToken: sessionToken,
                    apiBaseURL: apiBaseURL,
                    pollIntervalSeconds: configuredPollIntervalSeconds
                )
                completionHandler(.newData)
            } catch RequestError.unauthorized {
                completionHandler(.noData)
            } catch {
                completionHandler(.failed)
            }
        }
    }

    private func fetchLatestStatusEvent(sessionToken: String) async throws -> AgentWatchEvent? {
        var request = URLRequest(url: apiBaseURL.appendingPathComponent("v1/status"))
        request.timeoutInterval = 5
        request.setValue("Bearer \(sessionToken)", forHTTPHeaderField: "Authorization")

        let (data, response) = try await URLSession.shared.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw RequestError.badServerResponse
        }

        switch httpResponse.statusCode {
        case 200:
            let decoder = JSONDecoder()
            decoder.dateDecodingStrategy = .iso8601
            let status = try decoder.decode(AgentWatchStatusResponse.self, from: data)
            return status.current
        case 401:
            throw RequestError.unauthorized
        default:
            throw RequestError.badServerResponse
        }
    }

    private var storedSessionToken: String? {
        let rawValue = (UserDefaults.standard.string(forKey: StorageKeys.sessionToken) ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return rawValue.isEmpty ? nil : rawValue
    }

    private var apiBaseURL: URL {
        let fromEnv = (ProcessInfo.processInfo.environment["AGENTWATCH_API_BASE_URL"] ?? "")
            .trimmingCharacters(in: .whitespacesAndNewlines)
        if let envURL = URL(string: fromEnv), let host = envURL.host, !host.isEmpty {
            return envURL
        }

        return URL(string: "https://agentwatch-api-production-39a1.up.railway.app")!
    }

    private var configuredPollIntervalSeconds: TimeInterval {
        let storedMilliseconds = UserDefaults.standard.integer(forKey: StorageKeys.pollIntervalMilliseconds)
        let milliseconds: Int
        if UserDefaults.standard.object(forKey: StorageKeys.pollIntervalMilliseconds) == nil {
            milliseconds = 1_000
        } else {
            milliseconds = min(2_000, max(1, storedMilliseconds))
        }
        return TimeInterval(milliseconds) / 1_000
    }
}
