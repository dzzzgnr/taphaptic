import Foundation
import SwiftUI

@main
struct AgentWatchPhoneApp: App {
    @UIApplicationDelegateAdaptor(AgentWatchPhoneAppDelegate.self) private var appDelegate
    @StateObject private var model = AgentWatchPhoneModel()

    var body: some Scene {
        WindowGroup {
            PhoneContentView()
                .environmentObject(model)
                .onOpenURL { url in
                    model.handleIncomingURL(url)
                }
                .onContinueUserActivity(NSUserActivityTypeBrowsingWeb) { activity in
                    guard let url = activity.webpageURL else {
                        return
                    }

                    model.handleIncomingURL(url)
                }
        }
    }
}
