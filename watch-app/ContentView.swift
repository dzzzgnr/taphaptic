 import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var model: AgentWatchModel
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.accessibilityReduceMotion) private var accessibilityReduceMotion
    @State private var completedPulse = false
    @State private var titleTextIsSuccess = false
    @State private var statusTextIsSuccess = false
    @State private var textStaggerToken = 0
    private let appBackgroundColor = Color.black
    private let accentColor = Color(red: 1.0, green: 0.4, blue: 0.2)
    private let statusTransitionAnimation = Animation.spring(response: 0.52, dampingFraction: 0.84, blendDuration: 0.12)
    private let statusTextStaggerDelaySeconds: TimeInterval = 0.12
    private let keypadRows: [[PairingKey]] = [
        [.digit("1"), .digit("2"), .digit("3")],
        [.digit("4"), .digit("5"), .digit("6")],
        [.digit("7"), .digit("8"), .digit("9")],
        [.spacer, .digit("0"), .backspace],
    ]

    var body: some View {
        NavigationStack {
            GeometryReader { proxy in
                let screenWidth = max(0, proxy.size.width)
                let contentHeight = max(0, proxy.size.height)
                return Group {
                    if model.isPaired {
                        connectedSection
                    } else {
                        pairingKeypadOnly(
                            screenWidth: screenWidth,
                            contentHeight: contentHeight
                        )
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: model.isPaired ? .bottom : .top)
                .padding(.horizontal, model.isPaired ? 0 : 4)
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar(model.isPaired ? .visible : .hidden, for: .navigationBar)
            .toolbar {
                if model.isPaired {
                    ToolbarItem(placement: .topBarTrailing) {
                        NavigationLink {
                            WatchSettingsView()
                                .environmentObject(model)
                        } label: {
                            Image(systemName: "gear")
                        }
                        .accessibilityLabel("Settings")
                    }
                }
            }
        }
        .background(appBackgroundColor.ignoresSafeArea())
        .onAppear {
            model.setActive(true)
            syncTextTransitionState(to: model.displayState == .success)
        }
        .onDisappear {
            model.setActive(false)
        }
        .onChange(of: scenePhase) { _, newPhase in
            model.setActive(newPhase == .active)
        }
        .onChange(of: model.displayState) { oldValue, newValue in
            let oldIsAnimatedState = oldValue == .waiting || oldValue == .success
            let newIsAnimatedState = newValue == .waiting || newValue == .success
            let isSuccess = newValue == .success

            if oldIsAnimatedState && newIsAnimatedState {
                stageTextTransition(to: isSuccess)
            } else {
                syncTextTransitionState(to: isSuccess)
            }
        }
        .onChange(of: model.completedPulseToken) { _, _ in
            completedPulse = false
            withAnimation(.spring(response: 0.35, dampingFraction: 0.5)) {
                completedPulse = true
            }
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) {
                withAnimation(.easeOut(duration: 0.2)) {
                    completedPulse = false
                }
            }
        }
        .onChange(of: model.pairingCode) { _, newValue in
            guard !model.isPaired else {
                return
            }
            guard !model.isPairingInProgress else {
                return
            }
            if newValue.count == 4 {
                model.submitPairingCode()
            }
        }
    }

    private func pairingKeypadOnly(
        screenWidth: CGFloat,
        contentHeight: CGFloat
    ) -> some View {
        let isCompactLayout = screenWidth <= 174
        let slotSize: CGFloat = isCompactLayout ? 10 : 11
        let slotSpacing: CGFloat = isCompactLayout ? 9 : 10
        let dotsBandHeight: CGFloat = 32
        let dotsYOffset: CGFloat = -12
        let statusFontSize: CGFloat = isCompactLayout ? 13 : 15
        let pairingStatus = pairingInlineStatus
        let statusBandHeight: CGFloat = 8
        let statusBandYOffset: CGFloat = -2
        let dotsToStatusGap: CGFloat = 0
        let statusToKeypadGap: CGFloat = 4
        let keySpacing: CGFloat = 4
        let keypadRowCount: CGFloat = 4
        let topSectionHeight = dotsBandHeight + dotsToStatusGap + statusBandHeight + statusToKeypadGap
        let extraKeypadHeight: CGFloat = {
            if isCompactLayout {
                return 19
            }
            if screenWidth >= 205 {
                return 36
            }
            return 22
        }()
        let availableForKeys = max(0, contentHeight - topSectionHeight + extraKeypadHeight)
        let keyHeight = max(0, (availableForKeys - keySpacing * (keypadRowCount - 1)) / keypadRowCount)
        let keyFontSize = max(isCompactLayout ? 18 : 20, min(28, floor(keyHeight * 0.52)))
        let keyFont: Font = .system(size: keyFontSize, weight: .medium)
        let enteredCodeFontSize: CGFloat = isCompactLayout ? 20 : 22

        return VStack(spacing: 0) {
            ZStack(alignment: .top) {
                pairingCodeSlots(slotSize: slotSize, slotSpacing: slotSpacing, enteredDigitFontSize: enteredCodeFontSize)
                    .padding(.top, 1)
            }
            .offset(y: dotsYOffset)
            .frame(height: dotsBandHeight)

            Color.clear
                .frame(height: dotsToStatusGap)

            if let pairingStatus {
                Text(pairingStatus.text)
                    .font(.system(size: statusFontSize, weight: .medium, design: .rounded))
                    .foregroundStyle(pairingStatus.color)
                    .lineLimit(1)
                    .minimumScaleFactor(0.85)
                    .frame(maxWidth: .infinity, alignment: .center)
                    .offset(y: -8)
                    .frame(height: statusBandHeight)
                    .offset(y: statusBandYOffset)
            }

            Color.clear
                .frame(height: statusToKeypadGap)

            keypad(keyHeight: keyHeight, keySpacing: keySpacing, keyFont: keyFont)
                .frame(height: availableForKeys, alignment: .top)

            Color.clear
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
    }

    private var pairingInlineStatus: (text: String, color: Color)? {
        switch model.pairingState {
        case .pairing:
            return ("Pairing...", .gray)
        case let .failed(message):
            let lowercased = message.lowercased()
            if lowercased.contains("expired") {
                return ("Code expired", .red)
            }
            if lowercased.contains("invalid") {
                return ("Invalid code", .red)
            }
            if lowercased.contains("already") {
                return ("Code already used", .red)
            }
            if lowercased.contains("too many") {
                return ("Try a new code", .red)
            }
            return ("Pairing failed", .red)
        case .notPaired:
            return (model.pairingHint, .gray)
        case .connected:
            return nil
        }
    }

    @ViewBuilder
    private var connectedSection: some View {
        if model.displayState == .waiting || model.displayState == .success {
            figmaAnimatedStatusScreen(isSuccess: model.displayState == .success)
                .offset(y: -8)
                .animation(statusTransitionAnimation, value: model.displayState)
        } else {
            legacyConnectedSection
        }
    }

    private var pendingHeadlineText: String {
        "GET NOTIFIED WHEN TASKS COMPLETE"
    }

    private func figmaAnimatedStatusScreen(isSuccess: Bool) -> some View {
        let connection = pendingConnectionStatus
        return figmaStatusSurface(contentTopPadding: 8, contentBottomPadding: 12) {
            VStack(alignment: .leading, spacing: 6) {
                ZStack {
                    pendingOrbitIcon
                        .opacity(isSuccess ? 0 : 1)
                        .scaleEffect(isSuccess ? 0.82 : 1.0)
                        .accessibilityHidden(isSuccess)

                    Circle()
                        .fill(accentColor)
                        .opacity(isSuccess ? 1 : 0)
                        .scaleEffect(isSuccess ? 1.0 : 0.64)

                    Image(systemName: "checkmark")
                        .font(.system(size: 32, weight: .semibold, design: .rounded))
                        .foregroundStyle(.black)
                        .opacity(isSuccess ? 1 : 0)
                        .scaleEffect(isSuccess ? 1.0 : 0.62)
                }
                .frame(width: 56, height: 56, alignment: .center)
                .scaleEffect(isSuccess && completedPulse ? 1.1 : 1.0)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.bottom, 4)

                ZStack(alignment: .topLeading) {
                    fixedConnectedHeadline(pendingHeadlineText, color: .white)
                        .hidden()
                        .accessibilityHidden(true)
                        .allowsHitTesting(false)

                    fixedConnectedHeadline(completedHeadlineText, color: accentColor)
                        .hidden()
                        .accessibilityHidden(true)
                        .allowsHitTesting(false)

                    if titleTextIsSuccess {
                        fixedConnectedHeadline(completedHeadlineText, color: accentColor)
                            .transition(.blurTextSwap(reduceMotion: accessibilityReduceMotion))
                    } else {
                        fixedConnectedHeadline(pendingHeadlineText, color: .white)
                            .transition(.blurTextSwap(reduceMotion: accessibilityReduceMotion))
                    }
                }
                .frame(maxWidth: .infinity, alignment: .topLeading)

                ZStack(alignment: .leading) {
                    Text(connection.text)
                        .font(.system(size: 16, weight: .medium, design: .rounded))
                        .foregroundStyle(.white.opacity(0.48))
                        .lineLimit(1)
                        .minimumScaleFactor(0.9)
                        .overlay(alignment: .leading) {
                            Circle()
                                .fill(connection.dotColor)
                                .frame(width: 6, height: 6)
                                .offset(x: -14)
                        }
                        .padding(.leading, 14)
                        .hidden()
                        .accessibilityHidden(true)
                        .allowsHitTesting(false)

                    Text("Task completed")
                        .font(.system(size: 16, weight: .medium, design: .rounded))
                        .foregroundStyle(.white.opacity(0.48))
                        .lineLimit(1)
                        .minimumScaleFactor(0.9)
                        .hidden()
                        .accessibilityHidden(true)
                        .allowsHitTesting(false)

                    if statusTextIsSuccess {
                        Text("Task completed")
                            .font(.system(size: 16, weight: .medium, design: .rounded))
                            .foregroundStyle(.white.opacity(0.48))
                            .lineLimit(1)
                            .minimumScaleFactor(0.9)
                            .transition(.blurTextSwap(reduceMotion: accessibilityReduceMotion))
                    } else {
                        Text(connection.text)
                            .font(.system(size: 16, weight: .medium, design: .rounded))
                            .foregroundStyle(.white.opacity(0.48))
                            .lineLimit(1)
                            .minimumScaleFactor(0.9)
                            .overlay(alignment: .leading) {
                                Circle()
                                    .fill(connection.dotColor)
                                    .frame(width: 6, height: 6)
                                    .offset(x: -14)
                            }
                            .padding(.leading, 14)
                            .transition(.blurTextSwap(reduceMotion: accessibilityReduceMotion))
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .animation(statusTransitionAnimation, value: isSuccess)
        }
    }

    private var legacyConnectedSection: some View {
        VStack(spacing: 10) {
            statusBadge

            ZStack(alignment: .top) {
                legacyConnectedHeadlineSizingTemplate()
                    .hidden()
                    .accessibilityHidden(true)
                    .allowsHitTesting(false)

                legacyConnectedHeadline(model.displayState.title)
                    .hidden()
                    .accessibilityHidden(true)
                    .allowsHitTesting(false)

                legacyConnectedHeadline(model.displayState.title)
            }
            .frame(maxWidth: .infinity, alignment: .top)

            if shouldShowPairedDetailText {
                Text(model.detailText)
                    .font(.caption2)
                    .multilineTextAlignment(.center)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
    }

    private var completedHeadlineText: String {
        let detail = model.detailText.trimmingCharacters(in: .whitespacesAndNewlines)
        if detail.isEmpty || detail.lowercased() == model.displayState.title.lowercased() {
            return "YOUR AGENT IS DONE"
        }
        return detail.uppercased()
    }

    private func fixedConnectedHeadline(_ text: String, color: Color) -> some View {
        let lines = balancedHeadlineLines(text, maxLines: 3, targetLineLength: 14)
        return VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                Text(line)
                    .font(.system(size: 22, weight: .semibold, design: .rounded))
                    .foregroundStyle(color)
                    .tracking(-0.22)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .layoutPriority(1)
    }

    private func legacyConnectedHeadline(_ text: String) -> some View {
        let lines = balancedHeadlineLines(text, maxLines: 3, targetLineLength: 18)
        return VStack(alignment: .center, spacing: 0) {
            ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                Text(line)
                    .font(.headline)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .frame(maxWidth: .infinity, alignment: .center)
            }
        }
        .frame(maxWidth: .infinity, alignment: .top)
    }

    private func legacyConnectedHeadlineSizingTemplate() -> some View {
        let lines = ["TITLE", "TITLE", "TITLE"]
        return VStack(alignment: .center, spacing: 0) {
            ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                Text(line)
                    .font(.headline)
                    .lineLimit(1)
                    .truncationMode(.tail)
                    .frame(maxWidth: .infinity, alignment: .center)
            }
        }
        .frame(maxWidth: .infinity, alignment: .top)
    }

    private var pendingConnectionStatus: (text: String, dotColor: Color) {
        switch model.terminalPresence {
        case .connected:
            return ("Connected", Color(red: 0.0, green: 1.0, blue: 0.22))
        case .stale:
            return ("No recent activity", accentColor)
        case .waitingFirstEvent:
            return ("Waiting for first event", .gray)
        }
    }

    private func figmaStatusSurface<Content: View>(
        contentTopPadding: CGFloat,
        contentBottomPadding: CGFloat,
        @ViewBuilder content: () -> Content
    ) -> some View {
        ZStack(alignment: .bottomLeading) {
            RoundedRectangle(cornerRadius: 36, style: .continuous)
                .fill(appBackgroundColor)

            content()
                .padding(.top, contentTopPadding)
                .padding(.horizontal, 12)
                .padding(.bottom, contentBottomPadding)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomLeading)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var pendingOrbitIcon: some View {
        TimelineView(.animation(minimumInterval: 1.0 / 30.0, paused: false)) { context in
            let dotCount = 12
            let jumpInterval = 2.0
            let cycleSeconds = 5.2 * 1.5
            let timestamp = context.date.timeIntervalSinceReferenceDate
            let stepProgress = timestamp / jumpInterval
            let step = floor(stepProgress)
            let phase = stepProgress - step
            let transitionWindow = 0.32
            let transitionProgress = min(max(phase / transitionWindow, 0), 1)
            let easedTransition = transitionProgress * transitionProgress * (3 - (2 * transitionProgress))
            let fromIndex = ((Int(step) % dotCount) + dotCount) % dotCount
            let accentIndexPosition = Double(fromIndex) + (phase < transitionWindow ? easedTransition : 1.0)
            let accentAngle = accentIndexPosition * (360.0 / Double(dotCount))
            let smoothProgress = timestamp
                .truncatingRemainder(dividingBy: cycleSeconds) / cycleSeconds
            let hopOffset = phase < transitionWindow ? sin(easedTransition * .pi) * 3.5 : 0

            ZStack {
                ForEach(0..<dotCount, id: \.self) { index in
                    Circle()
                        .fill(Color.white.opacity(0.42))
                        .frame(width: 4, height: 4)
                        .offset(y: -24)
                        .rotationEffect(.degrees(Double(index) * (360.0 / Double(dotCount))))
                }

                Circle()
                    .fill(accentColor)
                    .frame(width: 6, height: 6)
                    .offset(y: -(24 + hopOffset))
                    .rotationEffect(.degrees(accentAngle))
            }
            .rotationEffect(.degrees(smoothProgress * 360))
            .frame(width: 56, height: 56)
        }
    }

    private var shouldShowPairedDetailText: Bool {
        guard model.displayState != .waiting else {
            return false
        }
        let detail = model.detailText
        return !detail.isEmpty && detail.lowercased() != model.displayState.title.lowercased()
    }

    private func syncTextTransitionState(to isSuccess: Bool) {
        textStaggerToken += 1
        titleTextIsSuccess = isSuccess
        statusTextIsSuccess = isSuccess
    }

    private func stageTextTransition(to isSuccess: Bool) {
        textStaggerToken += 1
        let token = textStaggerToken
        titleTextIsSuccess = isSuccess

        DispatchQueue.main.asyncAfter(deadline: .now() + statusTextStaggerDelaySeconds) {
            guard token == textStaggerToken else {
                return
            }
            statusTextIsSuccess = isSuccess
        }
    }

    private func balancedHeadlineLines(_ text: String, maxLines: Int, targetLineLength: Int) -> [String] {
        let words = text
            .split(whereSeparator: \.isWhitespace)
            .map(String.init)

        guard !words.isEmpty else {
            return [""]
        }

        let allowedLines = max(1, min(maxLines, words.count))
        var bestLines = [words.joined(separator: " ")]
        var bestScore = scoreHeadlineLines(bestLines, targetLineLength: targetLineLength)

        for lineCount in 1...allowedLines {
            for breakpoints in candidateBreakpoints(wordCount: words.count, lineCount: lineCount) {
                let lines = buildHeadlineLines(words: words, breakpoints: breakpoints)
                let score = scoreHeadlineLines(lines, targetLineLength: targetLineLength)
                if score < bestScore - 0.0001 || (abs(score - bestScore) <= 0.0001 && prefersHeadlineLines(lines, over: bestLines)) {
                    bestScore = score
                    bestLines = lines
                }
            }
        }

        return bestLines
    }

    private func candidateBreakpoints(wordCount: Int, lineCount: Int) -> [[Int]] {
        guard lineCount > 1 else {
            return [[]]
        }

        let breakCount = lineCount - 1
        var results: [[Int]] = []

        func recurse(start: Int, remaining: Int, current: [Int]) {
            if remaining == 0 {
                results.append(current)
                return
            }

            let minIndex = start
            let maxIndex = wordCount - remaining
            guard minIndex <= maxIndex else {
                return
            }

            for index in minIndex...maxIndex {
                recurse(start: index + 1, remaining: remaining - 1, current: current + [index])
            }
        }

        recurse(start: 1, remaining: breakCount, current: [])
        return results
    }

    private func buildHeadlineLines(words: [String], breakpoints: [Int]) -> [String] {
        let boundaries = [0] + breakpoints + [words.count]
        var lines: [String] = []
        lines.reserveCapacity(max(1, boundaries.count - 1))

        for index in 0..<(boundaries.count - 1) {
            let start = boundaries[index]
            let end = boundaries[index + 1]
            lines.append(words[start..<end].joined(separator: " "))
        }

        return lines
    }

    private func scoreHeadlineLines(_ lines: [String], targetLineLength: Int) -> Double {
        guard !lines.isEmpty else {
            return 0
        }

        let lengths = lines.map { Double($0.count) }
        let target = Double(targetLineLength)
        let mean = lengths.reduce(0, +) / Double(lengths.count)
        let variance = lengths.reduce(0) { partial, value in
            let delta = value - mean
            return partial + (delta * delta)
        }

        let overflowPenalty = lengths.reduce(0) { partial, value in
            let overflow = max(0, value - target)
            return partial + (overflow * overflow * 3.5)
        }

        let shortLastPenalty: Double = {
            guard lengths.count > 1, let last = lengths.last else {
                return 0
            }
            let desiredMinimum = min(target, mean) * 0.7
            let shortfall = max(0, desiredMinimum - last)
            return shortfall * shortfall * 6
        }()

        let lineCountPenalty = Double(lines.count - 1) * 0.2
        return variance + overflowPenalty + shortLastPenalty + lineCountPenalty
    }

    private func prefersHeadlineLines(_ lhs: [String], over rhs: [String]) -> Bool {
        let lhsMax = lhs.map(\.count).max() ?? 0
        let rhsMax = rhs.map(\.count).max() ?? 0
        if lhsMax != rhsMax {
            return lhsMax < rhsMax
        }

        if lhs.count != rhs.count {
            return lhs.count > rhs.count
        }

        let lhsLast = lhs.last?.count ?? 0
        let rhsLast = rhs.last?.count ?? 0
        return lhsLast > rhsLast
    }

    private func pairingCodeSlots(slotSize: CGFloat, slotSpacing: CGFloat, enteredDigitFontSize: CGFloat) -> some View {
        let enteredSlotSize = slotSize + 2
        return HStack(spacing: slotSpacing) {
            ForEach(0..<4, id: \.self) { index in
                if index < model.pairingCode.count {
                    Text(model.pairingDigit(at: index))
                        .font(.system(size: enteredDigitFontSize, weight: .semibold, design: .rounded))
                        .monospacedDigit()
                        .foregroundStyle(.white)
                        .frame(width: enteredSlotSize, height: enteredSlotSize)
                } else {
                    Circle()
                        .fill(Color(red: 0.27, green: 0.28, blue: 0.30))
                        .frame(width: slotSize, height: slotSize)
                        .frame(width: enteredSlotSize, height: enteredSlotSize)
                }
            }
        }
    }

    private func keypad(keyHeight: CGFloat, keySpacing: CGFloat, keyFont: Font) -> some View {
        VStack(spacing: keySpacing) {
            ForEach(0..<keypadRows.count, id: \.self) { row in
                HStack(spacing: keySpacing) {
                    ForEach(0..<keypadRows[row].count, id: \.self) { column in
                        keypadButton(for: keypadRows[row][column], keyHeight: keyHeight, keyFont: keyFont)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func keypadButton(for key: PairingKey, keyHeight: CGFloat, keyFont: Font) -> some View {
        switch key {
        case let .digit(value):
            let isFailedPairing: Bool = {
                if case .failed = model.pairingState {
                    return true
                }
                return false
            }()
            let isDisabled = model.isPairingInProgress || (model.pairingCode.count >= 4 && !isFailedPairing)
            Button {
                model.appendPairingDigit(value)
            } label: {
                ZStack {
                    RoundedRectangle(cornerRadius: 100, style: .continuous)
                        .fill(Color(red: 0.27, green: 0.28, blue: 0.30))
                    Text(value)
                        .font(keyFont)
                        .foregroundStyle(.white)
                }
                .frame(maxWidth: .infinity, minHeight: keyHeight, maxHeight: keyHeight)
            }
            .buttonStyle(.plain)
            .opacity(isDisabled ? 0.64 : 1)
            .disabled(isDisabled)
        case .backspace:
            let isDisabled = model.isPairingInProgress || model.pairingCode.isEmpty
            Button {
                guard !isDisabled else {
                    return
                }
                model.removeLastPairingDigit()
            } label: {
                ZStack {
                    Image(systemName: "chevron.left")
                        .font(.system(size: keyHeight * 0.64, weight: .semibold, design: .rounded))
                        .foregroundColor(.white)
                }
                .frame(maxWidth: .infinity, minHeight: keyHeight, maxHeight: keyHeight)
                .contentShape(RoundedRectangle(cornerRadius: 100, style: .continuous))
            }
            .buttonStyle(.plain)
            .opacity(1)
        case .spacer:
            Color.clear
                .frame(maxWidth: .infinity, minHeight: keyHeight, maxHeight: keyHeight)
        }
    }

    private var statusBadge: some View {
        ZStack {
            if model.displayState == .waiting {
                TimelineView(.animation) { context in
                    let cycleSeconds = 0.9
                    let progress = context.date.timeIntervalSinceReferenceDate
                        .truncatingRemainder(dividingBy: cycleSeconds) / cycleSeconds
                    Circle()
                        .trim(from: 0.14, to: 0.9)
                        .stroke(
                            AngularGradient(
                                colors: [
                                    Color(red: 0.20, green: 0.63, blue: 0.98),
                                    Color(red: 0.41, green: 0.95, blue: 0.74),
                                    Color(red: 0.20, green: 0.63, blue: 0.98),
                                ],
                                center: .center
                            ),
                            style: StrokeStyle(lineWidth: 4, lineCap: .round)
                        )
                        .frame(width: 44, height: 44)
                        .rotationEffect(.degrees(progress * 360))
                }
            } else if model.displayState == .success {
                Circle()
                    .fill(.green)
                    .frame(width: 56, height: 56)

                Image(systemName: "checkmark")
                    .font(.system(size: 20, weight: .bold, design: .rounded))
                    .foregroundStyle(.white)
            } else {
                Circle()
                    .fill(model.displayState.color.opacity(0.28))
                    .frame(width: 56, height: 56)

                Image(systemName: model.displayState.symbolName)
                    .font(.title3.bold())
                    .foregroundStyle(model.displayState.color)
            }
        }
        .frame(width: 56, height: 56)
        .scaleEffect(completedPulse && model.displayState == .success ? 1.18 : 1.0)
    }
}

private struct WatchSettingsView: View {
    @EnvironmentObject private var model: AgentWatchModel
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 12) {
            Toggle(
                "Sound",
                isOn: Binding(
                    get: { model.watchSoundEnabled },
                    set: { model.setWatchSoundEnabled($0) }
                )
            )

            Toggle(
                "Haptic",
                isOn: Binding(
                    get: { model.watchHapticEnabled },
                    set: { model.setWatchHapticEnabled($0) }
                )
            )

            Spacer(minLength: 0)

            Button("Reset app") {
                model.resetPairing()
                dismiss()
            }
            .buttonStyle(.bordered)
            .frame(maxWidth: .infinity)
        }
        .padding(.horizontal)
        .padding(.top)
        .padding(.bottom, 8)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        .navigationTitle("Settings")
        .toolbar(.visible, for: .navigationBar)
        .ignoresSafeArea(.container, edges: .bottom)
    }
}

private enum PairingKey: Equatable {
    case digit(String)
    case backspace
    case spacer
}

#Preview {
    ContentView()
        .environmentObject(AgentWatchModel())
}
