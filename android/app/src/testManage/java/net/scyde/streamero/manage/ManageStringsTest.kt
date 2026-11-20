package net.scyde.streamero.manage

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/** The Manage strings: the same keys in English and Croatian, and counts as plurals. */
class ManageStringsTest {
    private fun res(path: String): File =
        listOf(File("src/manage/res/$path"), File("app/src/manage/res/$path"), File("android/app/src/manage/res/$path")).first { it.exists() }

    private fun elements(path: String, tag: String): List<Element> {
        val doc = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(res(path))
        val nodes = doc.getElementsByTagName(tag)
        return (0 until nodes.length).map { nodes.item(it) as Element }
    }

    private fun names(path: String, tag: String) = elements(path, tag).filter { it.getAttribute("translatable") != "false" }.map { it.getAttribute("name") }.toSet()

    @Test fun englishAndCroatianHaveTheSameKeys() {
        assertEquals(names("values/strings.xml", "string"), names("values-hr/strings.xml", "string"))
        assertEquals(names("values/strings.xml", "plurals"), names("values-hr/strings.xml", "plurals"))
    }

    @Test fun croatianPluralsHaveAllThreeForms() {
        for (p in elements("values-hr/strings.xml", "plurals")) {
            val items = p.getElementsByTagName("item")
            val q = (0 until items.length).map { (items.item(it) as Element).getAttribute("quantity") }.toSet()
            assertEquals(p.getAttribute("name"), setOf("one", "few", "other"), q)
            // In Croatian "one" also covers 21, 31, ...: the number must be in the text.
            val one = (0 until items.length).map { items.item(it) as Element }.first { it.getAttribute("quantity") == "one" }
            assertTrue(p.getAttribute("name"), one.textContent.contains("%1\$d") || one.textContent.contains("%3\$d"))
        }
        for (p in elements("values/strings.xml", "plurals")) {
            val items = p.getElementsByTagName("item")
            val q = (0 until items.length).map { (items.item(it) as Element).getAttribute("quantity") }.toSet()
            assertEquals(p.getAttribute("name"), setOf("one", "other"), q)
        }
    }

    @Test fun countsAreNotPlainStrings() {
        // Strings with a number followed by a counted word belong in <plurals>.
        val counted = Regex("%\\d\\\$d (items?|stavk|warnings?|folders?|times|days|min)")
        for (path in listOf("values/strings.xml", "values-hr/strings.xml")) {
            for (s in elements(path, "string")) assertFalse("${s.getAttribute("name")} in $path", counted.containsMatchIn(s.textContent))
        }
    }

    @Test fun noOtherServerNameInUiText() {
        for (path in listOf("values/strings.xml", "values-hr/strings.xml")) {
            assertFalse(path, res(path).readText().contains("jellyfin", ignoreCase = true))
        }
    }
}
