import java.awt.*;
import java.awt.geom.*;
import java.awt.image.BufferedImage;
import java.io.File;
import javax.imageio.ImageIO;

/**
 * Renders the "Streamero Manage" icons from the brand icon (docs/brand/icon-1024.png): the
 * Streamero mark with a small control badge (three slider bars on a paper-white disc, cut
 * into the icon with a dark ring) in the lower right. Icon only, no text.
 *
 *   java -Djava.awt.headless=true tools/ManageIcon.java icon   ../docs/brand/icon-1024.png out.png 192
 *   java -Djava.awt.headless=true tools/ManageIcon.java banner ../docs/brand/icon-1024.png out.png 320 180
 *
 * The banner matches the standard app's (scripts/brand-icons.py): the rounded icon at 70 % of
 * the height, centred on the tile black. See tools/manage-icons.sh for all densities.
 */
public class ManageIcon {
    static final Color GOLD = new Color(0xD4A443);

    public static void main(String[] a) throws Exception {
        BufferedImage badged = badge(ImageIO.read(new File(a[1])));
        BufferedImage out;
        if (a[0].equals("icon")) {
            int s = Integer.parseInt(a[3]);
            out = scale(badged, s, s, BufferedImage.TYPE_INT_RGB);
        } else {
            int w = Integer.parseInt(a[3]), h = Integer.parseInt(a[4]);
            out = new BufferedImage(w, h, BufferedImage.TYPE_INT_RGB);
            Graphics2D g = hq(out.createGraphics());
            g.setColor(new Color(14, 14, 14));
            g.fillRect(0, 0, w, h);
            int s = (int) (h * 0.7);
            BufferedImage icon = scale(badged, s, s, BufferedImage.TYPE_INT_ARGB);
            // Rounded corners like the standard banner (radius 230 of 1024).
            BufferedImage rounded = new BufferedImage(s, s, BufferedImage.TYPE_INT_ARGB);
            Graphics2D r = hq(rounded.createGraphics());
            float rad = s * 230f / 1024f * 2;
            r.setClip(new RoundRectangle2D.Float(0, 0, s, s, rad, rad));
            r.drawImage(icon, 0, 0, null);
            r.dispose();
            g.drawImage(rounded, (w - s) / 2, (h - s) / 2, null);
            g.dispose();
        }
        File f = new File(a[2]).getAbsoluteFile();
        f.getParentFile().mkdirs();
        ImageIO.write(out, "png", f);
    }

    static Graphics2D hq(Graphics2D g) {
        g.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON);
        g.setRenderingHint(RenderingHints.KEY_RENDERING, RenderingHints.VALUE_RENDER_QUALITY);
        g.setRenderingHint(RenderingHints.KEY_INTERPOLATION, RenderingHints.VALUE_INTERPOLATION_BICUBIC);
        g.setRenderingHint(RenderingHints.KEY_STROKE_CONTROL, RenderingHints.VALUE_STROKE_PURE);
        return g;
    }

    /** The badge drawn on the full-size icon, so every density is a clean downscale. */
    static BufferedImage badge(BufferedImage src) {
        int n = src.getWidth();
        BufferedImage img = new BufferedImage(n, n, BufferedImage.TYPE_INT_RGB);
        Graphics2D g = hq(img.createGraphics());
        g.drawImage(src, 0, 0, null);
        float s = n / 192f;
        float r = 34 * s, cx = n - 46 * s, cy = n - 46 * s, ring = 6 * s;
        g.setColor(new Color(0x0A0807));
        g.fill(new Ellipse2D.Float(cx - r - ring, cy - r - ring, 2 * (r + ring), 2 * (r + ring)));
        g.setColor(new Color(0xF4EFE9));
        g.fill(new Ellipse2D.Float(cx - r, cy - r, 2 * r, 2 * r));
        float bw = 32 * s, bh = 4.2f * s, gap = 11 * s, kr = 5.2f * s;
        float[] knob = {0.28f, 0.72f, 0.46f};
        for (int i = 0; i < 3; i++) {
            float y = cy + (i - 1) * gap;
            g.setColor(new Color(0x2A2521));
            g.fill(new RoundRectangle2D.Float(cx - bw / 2, y - bh / 2, bw, bh, bh, bh));
            float kx = cx - bw / 2 + bw * knob[i];
            g.setColor(GOLD);
            g.fill(new Ellipse2D.Float(kx - kr, y - kr, 2 * kr, 2 * kr));
        }
        g.dispose();
        return img;
    }

    /** Downscale in halving steps (sharper than one bicubic jump from 1024 to 48). */
    static BufferedImage scale(BufferedImage src, int w, int h, int type) {
        BufferedImage cur = src;
        int cw = src.getWidth(), ch = src.getHeight();
        while (cw / 2 >= w && ch / 2 >= h) {
            cw /= 2; ch /= 2;
            cur = draw(cur, cw, ch, type);
        }
        return draw(cur, w, h, type);
    }

    static BufferedImage draw(BufferedImage src, int w, int h, int type) {
        BufferedImage out = new BufferedImage(w, h, type);
        Graphics2D g = hq(out.createGraphics());
        g.drawImage(src, 0, 0, w, h, null);
        g.dispose();
        return out;
    }
}
