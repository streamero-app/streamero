import AVFoundation
import AVKit
import SwiftUI
import UIKit

/// AVKit's full player (transport, AirPlay, Picture in Picture) inside SwiftUI. SwiftUI's
/// `VideoPlayer` offers no Picture in Picture, so the view controller is wrapped directly.
/// Taps are reported (without taking them from AVKit) so the app's own chrome can follow
/// AVKit's controls.
struct SystemVideoPlayer: UIViewControllerRepresentable {
    let player: AVPlayer
    /// Picture in Picture starts by itself when the app goes to the background.
    var autoPip = false
    var onTap: (CGPoint, CGSize) -> Void = { _, _ in }

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let vc = AVPlayerViewController()
        vc.player = player
        vc.allowsPictureInPicturePlayback = true
        vc.canStartPictureInPictureAutomaticallyFromInline = autoPip
        vc.videoGravity = .resizeAspect
        vc.view.backgroundColor = .black
        let tap = UITapGestureRecognizer(target: context.coordinator, action: #selector(Coordinator.tapped(_:)))
        tap.cancelsTouchesInView = false
        tap.delaysTouchesBegan = false
        tap.delegate = context.coordinator
        vc.view.addGestureRecognizer(tap)
        context.coordinator.onTap = onTap
        return vc
    }

    func updateUIViewController(_ vc: AVPlayerViewController, context: Context) {
        if vc.player !== player { vc.player = player }
        if vc.canStartPictureInPictureAutomaticallyFromInline != autoPip {
            vc.canStartPictureInPictureAutomaticallyFromInline = autoPip
        }
        context.coordinator.onTap = onTap
    }

    @MainActor
    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        var onTap: (CGPoint, CGSize) -> Void = { _, _ in }

        @objc func tapped(_ recognizer: UITapGestureRecognizer) {
            guard let view = recognizer.view else { return }
            onTap(recognizer.location(in: view), view.bounds.size)
        }

        // AVKit's own gestures keep working next to ours.
        nonisolated func gestureRecognizer(_ gestureRecognizer: UIGestureRecognizer,
                                           shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool {
            true
        }
    }
}

/// Shows and hides the app's chrome (title, close, track menus) together with AVKit's controls:
/// up while paused, gone a few seconds into playback, toggled by taps on the open picture.
@MainActor
@Observable
final class PlayerChrome {
    private(set) var visible = true
    private var hideTask: Task<Void, Never>?
    private var playing = false
    var hideAfter: Duration = .seconds(3.5)

    func tap(at point: CGPoint, in size: CGSize) {
        switch ControlsTap.action(at: point, in: size) {
        case .keep: show()
        case .toggle: visible ? hide() : show()
        }
    }

    /// Called every second with the player's state.
    func sync(playing: Bool) {
        let started = playing && !self.playing
        self.playing = playing
        if !playing {
            hideTask?.cancel()
            hideTask = nil
            if !visible { setVisible(true) }
        } else if started && visible {
            scheduleHide()
        }
    }

    func show() {
        setVisible(true)
        scheduleHide()
    }

    func hide() {
        hideTask?.cancel()
        hideTask = nil
        setVisible(false)
    }

    private func scheduleHide() {
        hideTask?.cancel()
        guard playing else { hideTask = nil; return }
        let delay = hideAfter
        hideTask = Task { [weak self] in
            try? await Task.sleep(for: delay)
            guard !Task.isCancelled, let self, self.playing else { return }
            self.setVisible(false)
        }
    }

    private func setVisible(_ on: Bool) {
        withAnimation(.easeOut(duration: on ? 0.18 : 0.35)) { visible = on }
    }
}

/// While a video plays, music is paused and its lock-screen controls step aside; both come back
/// (paused) when the video closes.
struct VideoAudioFocus: ViewModifier {
    let music: MusicPlayer?

    func body(content: Content) -> some View {
        content
            .onAppear {
                music?.pause()
                music?.suspendRemote()
            }
            .onDisappear { music?.resumeRemote() }
    }
}

extension View {
    func videoAudioFocus(_ music: MusicPlayer?) -> some View { modifier(VideoAudioFocus(music: music)) }
}

extension AVPlayerItem {
    /// Waits until the item can seek precisely (or failed, or the time ran out).
    @MainActor
    func waitUntilReady(timeout: Duration = .seconds(20)) async -> Bool {
        let clock = ContinuousClock()
        let deadline = clock.now.advanced(by: timeout)
        while status == .unknown && clock.now < deadline && !Task.isCancelled {
            try? await Task.sleep(for: .milliseconds(50))
        }
        return status == .readyToPlay
    }
}

extension AVPlayer {
    /// Starts at `ms` exactly: waits for the item to be ready, then seeks without tolerance.
    @MainActor
    func seekWhenReady(toMs ms: Int64) async {
        guard ms > 0, let item = currentItem else { return }
        _ = await item.waitUntilReady()
        _ = await seek(to: CMTime(value: ms, timescale: 1000), toleranceBefore: .zero, toleranceAfter: .zero)
    }
}
