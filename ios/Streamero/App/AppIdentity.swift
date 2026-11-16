import Foundation

/// Which of the two apps built from these sources is running (D-048). Streamero Manage is
/// compiled with the MANAGE condition and adds control of the *arr stack for administrators.
enum AppIdentity {
    #if MANAGE
    static let isManage = true
    /// The app's own URL scheme (invite links); distinct so the two apps never collide.
    static let urlScheme = "streamero-manage"
    #else
    static let isManage = false
    static let urlScheme = "streamero"
    #endif
}
