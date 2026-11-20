package net.scyde.streamero.offline

import org.junit.Assert.assertEquals
import org.junit.Test

class DownloadRangeTest {
    @Test fun rangeNotSatisfiableWithTheWholeFileIsComplete() {
        assertEquals(RangeOutcome.Complete, rangeOutcome(416, 1000, 1000))
        assertEquals(RangeOutcome.Complete, rangeOutcome(416, 1000, null))
    }

    @Test fun rangeNotSatisfiableWithAMismatchedPartStartsOver() {
        // A file that shrank on the server, or a part from an earlier version of it.
        assertEquals(RangeOutcome.Restart, rangeOutcome(416, 1500, 1000))
        assertEquals(RangeOutcome.Restart, rangeOutcome(416, 500, 1000))
    }

    @Test fun otherStatusesAreLeftToTheCaller() {
        assertEquals(RangeOutcome.Continue, rangeOutcome(206, 500, 1000))
        assertEquals(RangeOutcome.Continue, rangeOutcome(200, 0, 1000))
        assertEquals(RangeOutcome.Continue, rangeOutcome(416, 0, 1000))
    }

    @Test fun totalWithUnknownLengthFallsBackToTheJobSize() {
        // Before, a resumed download without Content-Length got a total of have - 1.
        assertEquals(1000L, downloadTotal(append = true, have = 400, contentLength = -1, sizeBytes = 1000))
        assertEquals(0L, downloadTotal(append = true, have = 400, contentLength = -1, sizeBytes = null))
        assertEquals(1000L, downloadTotal(append = true, have = 400, contentLength = 600, sizeBytes = 1000))
        assertEquals(900L, downloadTotal(append = false, have = 0, contentLength = 900, sizeBytes = 1000))
    }
}
