import Foundation
import WatchConnectivity

@MainActor
final class AgentWatchPhoneBridge: NSObject {
    private var session: WCSession?
    private var pendingContext: [String: Any]?
    private var lastSentContextFingerprint: String?

    override init() {
        super.init()
        activateIfNeeded()
    }

    func pushCurrentEvent(_ event: AgentWatchEvent?) {
        pushSyncState(event, phoneSessionToken: nil, apiBaseURL: nil, pollIntervalSeconds: nil)
    }

    func pushSyncState(_ event: AgentWatchEvent?, phoneSessionToken: String?, apiBaseURL: URL?, pollIntervalSeconds: TimeInterval?) {
        let state = AgentWatchPhoneSyncState(
            event: event,
            phoneSessionToken: phoneSessionToken,
            apiBaseURL: apiBaseURL?.absoluteString,
            pollIntervalSeconds: pollIntervalSeconds
        )
        let context = state.applicationContext
        push(context: context)
    }

    private func push(context: [String: Any]) {
        push(context: context, allowDuplicate: false)
    }

    private func activateIfNeeded() {
        guard WCSession.isSupported() else {
            return
        }

        let session = WCSession.default
        session.delegate = self
        session.activate()
        self.session = session
    }

    private func flushPendingContextIfNeeded() {
        guard let pendingContext else {
            return
        }

        push(context: pendingContext, allowDuplicate: true)
    }

    private func push(context: [String: Any], allowDuplicate: Bool) {
        guard let session else {
            pendingContext = context
            return
        }

        guard session.activationState == .activated else {
            pendingContext = context
            session.activate()
            return
        }

        let fingerprint = contextFingerprint(context)
        if !allowDuplicate, fingerprint == lastSentContextFingerprint {
            pendingContext = nil
            return
        }

        do {
            try session.updateApplicationContext(context)
            lastSentContextFingerprint = fingerprint
            pendingContext = nil
        } catch {
            pendingContext = context
            #if DEBUG
            print("AgentWatchPhoneBridge update failed: \(error)")
            #endif
        }
    }

    private func contextFingerprint(_ context: [String: Any]) -> String {
        let state = AgentWatchPhoneSyncState(context: context)
        let payload = state.payload
        let payloadMarker = payload == nil ? "0" : "1"
        let payloadID = payload.map { String($0.id) } ?? ""
        let payloadType = payload?.type.rawValue ?? ""
        let payloadCreatedAt = payload?.createdAt ?? ""
        let payloadSource = payload?.source ?? ""
        let payloadTitle = payload?.title ?? ""
        let payloadBody = payload?.body ?? ""
        let phoneSessionToken = state.phoneSessionToken ?? ""
        let apiBaseURL = state.apiBaseURL ?? ""
        let pollIntervalSeconds = state.pollIntervalSeconds.map { String($0) } ?? ""

        return [
            payloadMarker,
            payloadID,
            payloadType,
            payloadCreatedAt,
            payloadSource,
            payloadTitle,
            payloadBody,
            phoneSessionToken,
            apiBaseURL,
            pollIntervalSeconds,
        ].joined(separator: "|")
    }
}

extension AgentWatchPhoneBridge: WCSessionDelegate {
    nonisolated func sessionDidBecomeInactive(_ session: WCSession) {}

    nonisolated func sessionDidDeactivate(_ session: WCSession) {
        session.activate()
    }

    nonisolated func session(_ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: (any Error)?) {
        guard activationState == .activated else {
            return
        }

        Task { @MainActor [weak self] in
            self?.flushPendingContextIfNeeded()
        }
    }

    nonisolated func sessionWatchStateDidChange(_ session: WCSession) {
        Task { @MainActor [weak self] in
            self?.flushPendingContextIfNeeded()
        }
    }
}
