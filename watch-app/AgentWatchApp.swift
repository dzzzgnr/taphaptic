import SwiftUI

@main
struct AgentWatchApp: App {
    @StateObject private var model = AgentWatchModel()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(model)
        }
    }
}
