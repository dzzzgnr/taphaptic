import SwiftUI

@main
struct TaphapticApp: App {
    @StateObject private var model = AgentWatchModel()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(model)
        }
    }
}
