import Foundation
import UserNotifications
import WatchKit

@MainActor
final class TaphapticNotificationCoordinator: NSObject, ObservableObject {
    static let shared = TaphapticNotificationCoordinator()

    enum AuthorizationState: Equatable {
        case unknown
        case denied
        case authorized
        case provisional

        var label: String {
            switch self {
            case .unknown:
                return "Not requested"
            case .denied:
                return "Notifications off"
            case .authorized:
                return "Notifications on"
            case .provisional:
                return "Notifications provisional"
            }
        }
    }

    @Published private(set) var authorizationState: AuthorizationState = .unknown
    @Published private(set) var pushToken: String?
    @Published private(set) var registrationError: String?

    var onPushTokenChanged: ((String) -> Void)?
    var onRemoteEvent: (() -> Void)?

    private override init() {
        super.init()
    }

    func configure() {
        UNUserNotificationCenter.current().delegate = self
        Task {
            await refreshAuthorizationState()
        }
    }

    func requestAuthorization() async {
        do {
            let granted = try await UNUserNotificationCenter.current()
                .requestAuthorization(options: [.alert, .sound])
            await refreshAuthorizationState()
            if granted {
                WKApplication.shared().registerForRemoteNotifications()
            }
        } catch {
            registrationError = "Notification permission failed."
        }
    }

    func refreshAuthorizationState() async {
        let settings = await UNUserNotificationCenter.current().notificationSettings()
        switch settings.authorizationStatus {
        case .authorized:
            authorizationState = .authorized
            WKApplication.shared().registerForRemoteNotifications()
        case .provisional, .ephemeral:
            authorizationState = .provisional
            WKApplication.shared().registerForRemoteNotifications()
        case .denied:
            authorizationState = .denied
        case .notDetermined:
            authorizationState = .unknown
        @unknown default:
            authorizationState = .unknown
        }
    }

    func didRegister(deviceToken: Data) {
        let token = deviceToken.map { String(format: "%02x", $0) }.joined()
        guard !token.isEmpty else {
            return
        }
        pushToken = token
        registrationError = nil
        onPushTokenChanged?(token)
    }

    func didFailRegistration(error: Error) {
        registrationError = "Push registration failed."
    }

    func handleRemoteEvent() {
        onRemoteEvent?()
    }
}

extension TaphapticNotificationCoordinator: UNUserNotificationCenterDelegate {
    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        await MainActor.run {
            self.handleRemoteEvent()
        }
        return [.banner, .sound]
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse
    ) async {
        await MainActor.run {
            self.handleRemoteEvent()
        }
    }
}

@MainActor
final class TaphapticAppDelegate: NSObject, WKApplicationDelegate {
    private let notifications = TaphapticNotificationCoordinator.shared

    func applicationDidFinishLaunching() {
        notifications.configure()
    }

    func didRegisterForRemoteNotifications(withDeviceToken deviceToken: Data) {
        notifications.didRegister(deviceToken: deviceToken)
    }

    func didFailToRegisterForRemoteNotificationsWithError(_ error: Error) {
        notifications.didFailRegistration(error: error)
    }

    func didReceiveRemoteNotification(
        _ userInfo: [AnyHashable: Any],
        fetchCompletionHandler completionHandler: @escaping (WKBackgroundFetchResult) -> Void
    ) {
        notifications.handleRemoteEvent()
        completionHandler(.newData)
    }
}
