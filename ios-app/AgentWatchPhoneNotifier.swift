import Foundation
import UIKit
import UserNotifications

final class AgentWatchPhoneNotifier: NSObject {
    private let center = UNUserNotificationCenter.current()

    override init() {
        super.init()
        center.delegate = self
    }

    func notify(for event: AgentWatchEvent) {
        let content = UNMutableNotificationContent()
        content.title = event.resolvedTitle
        content.body = event.resolvedBody
        content.sound = .default
        content.threadIdentifier = "agentwatch"

        let request = UNNotificationRequest(
            identifier: "agentwatch-\(event.id)",
            content: content,
            trigger: UNTimeIntervalNotificationTrigger(timeInterval: 0.1, repeats: false)
        )

        center.add(request)
    }

    func ensureRemoteNotificationsRegistration() {
        center.getNotificationSettings { settings in
            switch settings.authorizationStatus {
            case .authorized, .provisional, .ephemeral:
                DispatchQueue.main.async {
                    UIApplication.shared.registerForRemoteNotifications()
                }
            case .notDetermined:
                UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .badge, .sound]) { granted, _ in
                    guard granted else {
                        return
                    }

                    DispatchQueue.main.async {
                        UIApplication.shared.registerForRemoteNotifications()
                    }
                }
            case .denied:
                break
            @unknown default:
                break
            }
        }
    }
}

extension AgentWatchPhoneNotifier: UNUserNotificationCenterDelegate {
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .list, .sound])
    }
}
