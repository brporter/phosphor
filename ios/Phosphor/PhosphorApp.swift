import SwiftUI

@main
struct PhosphorApp: App {
    @State private var auth = AuthViewModel()

    var body: some Scene {
        WindowGroup {
            ContentView(auth: auth)
                .preferredColorScheme(.dark)
                .onAppear {
                    auth.loadCachedUser()
                }
        }
    }
}
