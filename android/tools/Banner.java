import java.awt.*;
import java.awt.font.*;
import java.awt.geom.*;
import java.awt.image.BufferedImage;
import java.io.File;
import java.util.Map;
import javax.imageio.ImageIO;

/**
 * Renders the Android TV launcher banner (320x180 dp) from the app icon and the wordmark.
 *
 *   java -Djava.awt.headless=true tools/Banner.java app/src/main/res/mipmap-xxxhdpi/ic_launcher.png \
 *     app/src/main/res/drawable-xhdpi/banner.png 1 /usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf
 *   (and 1.5 for drawable-xxhdpi)
 */
public class Banner {
    public static void main(String[] a) throws Exception {
        BufferedImage src = ImageIO.read(new File(a[0]));
        // Keep only the bright play mark: the icon's own dark square would show as a box.
        BufferedImage icon = new BufferedImage(src.getWidth(), src.getHeight(), BufferedImage.TYPE_INT_ARGB);
        for (int y = 0; y < src.getHeight(); y++) for (int x = 0; x < src.getWidth(); x++) {
            int c = src.getRGB(x, y), r = (c >> 16) & 255;
            int al = Math.max(0, Math.min(255, (r - 110) * 255 / 70));
            icon.setRGB(x, y, (al << 24) | (c & 0xFFFFFF));
        }
        float s = Float.parseFloat(a[2]); // 1 = xhdpi 320x180
        int w = Math.round(320 * s), h = Math.round(180 * s);
        BufferedImage img = new BufferedImage(w, h, BufferedImage.TYPE_INT_RGB);
        Graphics2D g = img.createGraphics();
        g.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON);
        g.setRenderingHint(RenderingHints.KEY_TEXT_ANTIALIASING, RenderingHints.VALUE_TEXT_ANTIALIAS_ON);
        g.setRenderingHint(RenderingHints.KEY_INTERPOLATION, RenderingHints.VALUE_INTERPOLATION_BICUBIC);
        g.setRenderingHint(RenderingHints.KEY_RENDERING, RenderingHints.VALUE_RENDER_QUALITY);
        // Warm near-black with the projector glow of "Kino platno" from the lower left.
        g.setColor(new Color(0x0A0807));
        g.fillRect(0, 0, w, h);
        g.setPaint(new RadialGradientPaint(new Point2D.Float(w * 0.18f, h * 0.95f), w * 0.75f,
            new float[]{0f, 1f}, new Color[]{new Color(214, 118, 63, 70), new Color(10, 8, 7, 0)}));
        g.fillRect(0, 0, w, h);
        // The play mark from the launcher icon; its own background fades into the banner.
        int is = Math.round(118 * s);
        int ix = Math.round(10 * s), iy = (h - is) / 2;
        g.drawImage(icon, ix, iy, is, is, null);
        // Wordmark: heavy caps, slightly tracked, paper white.
        Font base = Font.createFont(Font.TRUETYPE_FONT, new File(a[3]));
        float size = 40 * s;
        Font f = base.deriveFont(size).deriveFont(Map.of(TextAttribute.TRACKING, 0.02f));
        g.setFont(f);
        String text = "STREAMERO";
        int tx = ix + is - Math.round(8 * s);
        int maxW = w - tx - Math.round(16 * s);
        while (g.getFontMetrics().stringWidth(text) > maxW) { size -= 0.5f; f = f.deriveFont(size); g.setFont(f); }
        FontMetrics fm = g.getFontMetrics();
        int ty = h / 2 + fm.getAscent() / 2 - Math.round(4 * s);
        g.setColor(new Color(0xF4EFE9));
        g.drawString(text, tx, ty);
        // A thin accent rule under the name, as on the title cards.
        g.setColor(new Color(0xD6763F));
        g.fillRect(tx + Math.round(2 * s), ty + Math.round(10 * s), Math.round(34 * s), Math.max(1, Math.round(3 * s)));
        g.dispose();
        ImageIO.write(img, "png", new File(a[1]));
    }
}
