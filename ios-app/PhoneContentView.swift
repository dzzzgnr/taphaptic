import SwiftUI

struct PhoneContentView: View {
    @EnvironmentObject private var model: AgentWatchPhoneModel
    @Environment(\.scenePhase) private var scenePhase

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    statusCard
                    onboardingCard
                    recentEventsSection
                }
                .padding(20)
            }
            .navigationTitle("AgentWatch")
        }
        .onAppear {
            model.setActive(true)
        }
        .onDisappear {
            model.setActive(false)
        }
        .onChange(of: scenePhase) { _, newPhase in
            model.setActive(newPhase == .active)
        }
    }

    private var statusCard: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 14) {
                Circle()
                    .fill(currentAccentColor.opacity(0.18))
                    .frame(width: 56, height: 56)
                    .overlay {
                        Image(systemName: currentSymbolName)
                            .font(.title2.weight(.semibold))
                            .foregroundStyle(currentAccentColor)
                    }

                VStack(alignment: .leading, spacing: 4) {
                    Text(model.currentEvent?.resolvedTitle ?? "Waiting")
                        .font(.headline)

                    Text(model.currentEvent?.resolvedBody ?? "After pairing, this iPhone syncs Claude status and forwards it to Apple Watch.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }

            Text(model.connectionSummary)
                .font(.footnote)
                .foregroundStyle(.secondary)

            Button {
                model.manualRefresh()
            } label: {
                HStack {
                    Image(systemName: "arrow.clockwise")
                    Text(model.isRefreshing ? "Refreshing..." : "Refresh Now")
                }
                .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .disabled(model.isRefreshing)
        }
        .padding(18)
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
    }

    private var onboardingCard: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Onboarding")
                .font(.headline)

            Text("1. In Claude Code run: `curl -fsSL https://agentwatchapp.vercel.app/install/claude | sh`\n2. Scan the QR link with iPhone Camera.\n3. Open AgentWatch. Pairing completes automatically.")
                .font(.footnote)
                .foregroundStyle(.secondary)

            Text(model.accountSummary)
                .font(.footnote)
                .foregroundStyle(.secondary)

            if model.isPairing {
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Pairing...")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
            }

            if model.isPaired {
                Button(role: .destructive) {
                    model.resetPairing()
                } label: {
                    Text("Reset Pairing")
                        .frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
            }

            VStack(alignment: .leading, spacing: 8) {
                Text("Poll cadence")
                    .font(.footnote.weight(.semibold))
                HStack(spacing: 8) {
                    Text("1ms")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                    Slider(
                        value: Binding(
                            get: { Double(model.pollIntervalMilliseconds) },
                            set: { model.setPollIntervalMilliseconds(Int($0.rounded())) }
                        ),
                        in: 1...2_000,
                        step: 1
                    )
                    Text("2s")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                Text("Current: \(model.pollIntervalSummary)")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            Text(model.pushSummary)
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .padding(18)
        .background(Color(uiColor: .secondarySystemBackground), in: RoundedRectangle(cornerRadius: 22, style: .continuous))
    }

    private var recentEventsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Recent Events")
                .font(.headline)

            if model.recentEvents.isEmpty {
                Text("No events yet. Once Claude sends status updates, they appear here and sync to watch.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(16)
                    .background(Color(uiColor: .secondarySystemBackground), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
            } else {
                VStack(spacing: 10) {
                    ForEach(model.recentEvents) { event in
                        EventRow(event: event)
                    }
                }
            }
        }
    }

    private var currentAccentColor: Color {
        guard let event = model.currentEvent else {
            return .gray
        }

        return accentColor(for: event.type)
    }

    private var currentSymbolName: String {
        model.currentEvent?.type.systemImageName ?? "ellipsis.circle.fill"
    }

    private func accentColor(for type: AgentWatchEventType) -> Color {
        switch type {
        case .completed:
            return .green
        case .subagentCompleted:
            return .mint
        case .failed:
            return .red
        case .attention:
            return .orange
        }
    }
}

private struct EventRow: View {
    let event: AgentWatchEvent

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: event.type.systemImageName)
                .font(.title3)
                .foregroundStyle(accentColor)

            VStack(alignment: .leading, spacing: 4) {
                Text(event.resolvedTitle)
                    .font(.subheadline.weight(.semibold))

                Text(event.resolvedBody)
                    .font(.footnote)
                    .foregroundStyle(.secondary)

                Text(event.createdAt.formatted(date: .abbreviated, time: .shortened))
                    .font(.caption)
                    .foregroundStyle(.tertiary)
            }

            Spacer(minLength: 0)
        }
        .padding(14)
        .background(Color(uiColor: .secondarySystemBackground), in: RoundedRectangle(cornerRadius: 18, style: .continuous))
    }

    private var accentColor: Color {
        switch event.type {
        case .completed:
            return .green
        case .subagentCompleted:
            return .mint
        case .failed:
            return .red
        case .attention:
            return .orange
        }
    }
}

#Preview {
    PhoneContentView()
        .environmentObject(AgentWatchPhoneModel())
}
