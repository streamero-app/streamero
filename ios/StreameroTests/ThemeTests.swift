import SwiftUI
import XCTest
@testable import Streamero

final class ThemeTests: XCTestCase {
    func testBrandTypeFollowsDynamicTypeWithACap() {
        XCTAssertEqual(Theme.scaledSize(30, for: .large, maxScale: 1.35), 30, accuracy: 0.5)
        XCTAssertGreaterThan(Theme.scaledSize(30, for: .xxxLarge, maxScale: 1.35), 30)
        XCTAssertLessThan(Theme.scaledSize(30, for: .xSmall, maxScale: 1.35), 30)
        XCTAssertEqual(Theme.scaledSize(30, for: .accessibility5, maxScale: 1.35), 40.5, accuracy: 0.01)
    }

    func testHeroFitsTheScreen() {
        // Tall phone: capped at the design height.
        XCTAssertEqual(Theme.heroHeight(container: 932, compactHeight: false, maximum: 560), 560)
        // iPhone SE portrait: most of the screen, not all of it.
        XCTAssertEqual(Theme.heroHeight(container: 667, compactHeight: false, maximum: 560), 667 * 0.72, accuracy: 0.01)
        // Landscape iPhone: well under the fixed 560 pt that used to overflow.
        let landscape = Theme.heroHeight(container: 390, compactHeight: true, maximum: 560)
        XCTAssertLessThan(landscape, 390)
        XCTAssertGreaterThanOrEqual(landscape, 220)
    }
}
