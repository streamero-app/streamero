import java.awt.*;
import java.awt.geom.*;
import java.awt.image.BufferedImage;
import java.io.File;
import javax.imageio.ImageIO;

/**
 * Renders the "Streamero Manage" launcher icon: the Streamero play mark with a small
 * control badge (three slider bars on a paper-white disc) in the lower right. Icon only, no text.
 *
 *   java -Djava.awt.headless=true tools/ManageIcon.java app/src/main/res/mipmap-xxxhdpi/ic_launcher.png \
 *     app/src/manage/res/mipmap-xxxhdpi/ic_launcher.png
 *
 * The TV banner is tools/Banner.java run on the result (see tools/manage-icons.sh).
 */
public class ManageIcon {
    public static void main(String[] a) throws Exception {
        BufferedImage src = ImageIO.read(new File(a[0]));
        int n = src.getWidth();
        BufferedImage img = new BufferedImage(n, n, BufferedImage.TYPE_INT_RGB);
        Graphics2D g = img.createGraphics();
        g.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON);
        g.setRenderingHint(RenderingHints.KEY_RENDERING, RenderingHints.VALUE_RENDER_QUALITY);
        g.setRenderingHint(RenderingHints.KEY_STROKE_CONTROL, RenderingHints.VALUE_STROKE_PURE);
        g.drawImage(src, 0, 0, null);
        float s = n / 192f;
        // Badge: a paper-white disc with a dark ring cut into the mark, three slider bars inside.
        float r = 34 * s, cx = n - 46 * s, cy = n - 46 * s, ring = 6 * s;
        g.setColor(new Color(0x0A0807));
        g.fill(new Ellipse2D.Float(cx - r - ring, cy - r - ring, 2 * (r + ring), 2 * (r + ring)));
        g.setColor(new Color(0xF4EFE9));
        g.fill(new Ellipse2D.Float(cx - r, cy - r, 2 * r, 2 * r));
        float bw = 32 * s, bh = Math.max(1.5f, 4.2f * s), gap = 11 * s;
        float[] knob = {0.28f, 0.72f, 0.46f};
        for (int i = 0; i < 3; i++) {
            float y = cy + (i - 1) * gap;
            g.setColor(new Color(0x2A2521));
            g.fill(new RoundRectangle2D.Float(cx - bw / 2, y - bh / 2, bw, bh, bh, bh));
            float kx = cx - bw / 2 + bw * knob[i], kr = Math.max(1.6f, 5.2f * s);
            g.setColor(new Color(0xD6763F));
            g.fill(new Ellipse2D.Float(kx - kr, y - kr, 2 * kr, 2 * kr));
        }
        g.dispose();
        new File(a[1]).getAbsoluteFile().getParentFile().mkdirs();
        ImageIO.write(img, "png", new File(a[1]));
    }
}
