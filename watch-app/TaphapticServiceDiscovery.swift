import Foundation
import Network

final class TaphapticServiceDiscovery: @unchecked Sendable {
    private let queue = DispatchQueue(label: "local.taphaptic.discovery")
    private var browser: NWBrowser?
    private var emittedURLs: Set<String> = []

    var onURLResolved: ((URL) -> Void)?
    var onAvailabilityChanged: ((Bool) -> Void)?

    func start() {
        guard browser == nil else {
            return
        }

        let parameters = NWParameters.tcp
        parameters.includePeerToPeer = true

        let browser = NWBrowser(for: .bonjour(type: "_taphaptic._tcp", domain: "local"), using: parameters)
        browser.stateUpdateHandler = { [weak self] state in
            guard let self else {
                return
            }
            switch state {
            case .failed, .cancelled:
                DispatchQueue.main.async {
                    self.onAvailabilityChanged?(false)
                }
            default:
                break
            }
        }

        browser.browseResultsChangedHandler = { [weak self] results, _ in
            self?.handleResults(results)
        }

        self.browser = browser
        browser.start(queue: queue)
    }

    func stop() {
        browser?.cancel()
        browser = nil
        emittedURLs.removeAll()
        onAvailabilityChanged?(false)
    }

    private func handleResults(_ results: Set<NWBrowser.Result>) {
        let urls = results.compactMap { result in
            url(for: result.endpoint)
        }

        DispatchQueue.main.async {
            self.onAvailabilityChanged?(!urls.isEmpty)

            for url in urls {
                let key = url.absoluteString
                if self.emittedURLs.contains(key) {
                    continue
                }
                self.emittedURLs.insert(key)
                self.onURLResolved?(url)
            }

            if urls.isEmpty {
                self.emittedURLs.removeAll()
            }
        }
    }

    private func url(for endpoint: NWEndpoint) -> URL? {
        switch endpoint {
        case let .hostPort(host, port):
            return buildURL(host: hostString(host), port: Int(port.rawValue))
        case let .service(name, _, domain, _):
            let normalizedName = name.trimmingCharacters(in: CharacterSet(charactersIn: "."))
            guard !normalizedName.isEmpty else {
                return nil
            }

            let normalizedDomain = domain.trimmingCharacters(in: CharacterSet(charactersIn: "."))
            let suffix = normalizedDomain.isEmpty ? "local" : normalizedDomain

            let host: String
            if normalizedName.contains(".") || normalizedName.hasSuffix(".\(suffix)") {
                host = normalizedName
            } else {
                host = "\(normalizedName).\(suffix)"
            }
            return buildURL(host: host, port: 8080)
        default:
            return nil
        }
    }

    private func buildURL(host: String, port: Int) -> URL? {
        let normalizedHost = host.trimmingCharacters(in: CharacterSet(charactersIn: "."))
        guard !normalizedHost.isEmpty else {
            return nil
        }

        var components = URLComponents()
        components.scheme = "http"
        components.host = normalizedHost
        components.port = port
        return components.url
    }

    private func hostString(_ host: NWEndpoint.Host) -> String {
        switch host {
        case let .name(name, _):
            return name
        case let .ipv4(address):
            return address.debugDescription
        case let .ipv6(address):
            return address.debugDescription
        @unknown default:
            return ""
        }
    }
}
