import SwiftUI

@main
struct TaphapticApp: App {
    @WKApplicationDelegateAdaptor(TaphapticAppDelegate.self) private var appDelegate
    @StateObject private var model = TaphapticModel()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(model)
        }
    }
}
