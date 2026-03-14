import SwiftUI

@main
struct TaphapticApp: App {
    @StateObject private var model = TaphapticModel()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(model)
        }
    }
}
