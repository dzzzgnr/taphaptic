import SwiftUI

enum BlurTextTransitionTokens {
    static let blurRadius: CGFloat = 8
    static let enterDuration: TimeInterval = 0.35
    static let exitDuration: TimeInterval = 0.30
    static let offsetY: CGFloat = 4
    static let reduceMotionDuration: TimeInterval = 0.15
}

private struct BlurTextTransitionModifier: ViewModifier {
    let blurRadius: CGFloat
    let opacity: Double
    let yOffset: CGFloat

    func body(content: Content) -> some View {
        content
            .blur(radius: blurRadius)
            .opacity(opacity)
            .offset(y: yOffset)
    }
}

extension AnyTransition {
    static func blurTextSwap(reduceMotion: Bool) -> AnyTransition {
        if reduceMotion {
            return .asymmetric(
                insertion: .opacity.animation(.easeOut(duration: BlurTextTransitionTokens.reduceMotionDuration)),
                removal: .opacity.animation(.easeOut(duration: BlurTextTransitionTokens.reduceMotionDuration))
            )
        }

        let insertion = AnyTransition.modifier(
            active: BlurTextTransitionModifier(
                blurRadius: BlurTextTransitionTokens.blurRadius,
                opacity: 0,
                yOffset: BlurTextTransitionTokens.offsetY
            ),
            identity: BlurTextTransitionModifier(blurRadius: 0, opacity: 1, yOffset: 0)
        )
        .animation(.easeOut(duration: BlurTextTransitionTokens.enterDuration))

        let removal = AnyTransition.modifier(
            active: BlurTextTransitionModifier(
                blurRadius: BlurTextTransitionTokens.blurRadius,
                opacity: 0,
                yOffset: -BlurTextTransitionTokens.offsetY
            ),
            identity: BlurTextTransitionModifier(blurRadius: 0, opacity: 1, yOffset: 0)
        )
        .animation(.easeOut(duration: BlurTextTransitionTokens.exitDuration))

        return .asymmetric(insertion: insertion, removal: removal)
    }
}
