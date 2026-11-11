// GENERATED FILE — do not edit. Run `npm run guides:sync` (web/scripts/guides.mjs).
//
// The PRIVATE guides: authored in docs/guides/<slug>/ exactly like the public ones, but with a
// page outside app/docs that is noindex and linked from nowhere in the repo. They are kept in a
// separate module so /docs/guides (which maps GUIDES) cannot list one by accident, and the docs
// registry, sitemap, search index and llms.txt never see them.
//
// Source of truth: docs/guides/<slug>/README.md + its screenshots. The screenshots are copied to
// web/public/<assets>/ (the `assets` field below is that directory), NOT to public/guides/.

export type PrivateGuideShot = {
  /** Bare filename, exactly as the README writes it: ![](01-step.png). */
  file: string;
  /** Intrinsic PNG size of the FULL capture — the zoom target, and the intrinsic size of the
   *  <a href> it is opened at. The figure itself renders `detail` when there is one. */
  width: number;
  height: number;
  /**
   * The INLINE figure: a crop of `file` (docs/guides/<slug>/crops.json), sized to stay legible at
   * the width of a phone — see src/lib/panduanSurau.test.ts for the scale floor it must meet.
   * `path` is relative to the guide's `assets` directory. null: the capture renders whole.
   */
  detail: PrivateGuideDetail | null;
};

export type PrivateGuideDetail = {
  /** Path under the guide's `assets` directory: "detail/<file>". */
  path: string;
  width: number;
  height: number;
};

export type PrivateGuide = {
  slug: string;
  /** The README's `# …` line. The page renders it as its <h1>; it is removed from body. */
  title: string;
  /** The README's first paragraph, rendered as the page's lede (and removed from body). */
  summary: string;
  /** The README with the title line and the lede removed, rendered through <Markdown mode="docs">. */
  body: string;
  shots: PrivateGuideShot[];
  /** Public directory the screenshots were copied to, e.g. "/panduan/surau". */
  assets: string;
};

export const PRIVATE_GUIDES: PrivateGuide[] = [
  {
    "slug": "surau-donation",
    "title": "Panduan derma surau: kutip derma dengan JomForm dan CHIP",
    "summary": "Sembilan langkah untuk jawatankuasa surau mengutip derma dalam talian dengan JomForm, dengan caj CHIP ditanggung pembayar — bukan oleh surau.",
    "body": "## 1. Daftar akaun surau\n\n![Halaman utama JomForm dengan butang Mula percuma dibulatkan](01-mula-percuma.png)\n\nBuka **jomform.com**, klik **Mula percuma →**, dan taip **e-mel kongsi jawatankuasa** — bukan e-mel peribadi seorang ahli. JomForm menghantar **pautan ajaib**; klik pautan itu dalam peti masuk kongsi untuk sahkan akaun. Tiada kata laluan untuk diingat atau dikongsi.\n\n## 2. Cipta workspace surau\n\n![Skrin cipta workspace dengan nama, slug dan butang Cipta dibulatkan](02-workspace-surau.png)\n\nIsi **Nama workspace** dengan nama penuh surau dan pilih slug URL, kemudian klik **Cipta**. Nama inilah yang pembayar nampak pada halaman bayaran.\n\n## 3. Jemput ahli jawatankuasa\n\n![Tetapan Ahli dengan medan e-mel, peranan dan butang Hantar jemputan dibulatkan](03-jemput-ahli.png)\n\nSettings → **Ahli** → taip e-mel bendahari atau imam, pilih **Peranan**, kemudian **Hantar jemputan**. Setiap ahli masuk dengan akaun sendiri, jadi tiada satu log masuk dikongsi.\n\n## 4. Sambungkan akaun pedagang CHIP\n\n![Tetapan CHIP dengan medan Brand ID dan Kunci Rahsia dibulatkan](04-chip-kunci.png)\n\nDalam Portal Pedagang CHIP → **Developers**, salin **Brand ID** dan **Secret Key**. Di JomForm: Settings → **CHIP**, tampal kedua-duanya, kemudian **Simpan**. JomForm mengesahkan pasangan itu dengan CHIP, menyimpannya **disulitkan**, dan hanya memaparkan hujung kunci (\"berakhir 4f2a\"). Tiada pilihan Mod: kunci itu sendiri menentukan sama ada bayaran itu ujian atau sebenar.\n\n## 5. Tetapkan caj CHIP pada pembayar\n\n![Tetapan Caj bayaran dengan baris FPX 1.00% dan pengakuan dibulatkan](05-caj-bayaran.png)\n\nSettings → **Caj bayaran**. Salin **dua nombor** daripada kontrak CHIP anda: **Tetap (sen)**, caj tetap setiap transaksi, dan **Peratus (basis point, 100 = 1%)**. Tandakan kaedah yang surau terima — **FPX** dan/atau **DuitNow QR** — tandakan **pengakuan**, kemudian **Simpan caj bayaran**.\n\nDengan caj diaktifkan, **pembayar** membayar caj itu dan surau menerima derma penuh. Dengan caj dimatikan, surau yang menanggungnya: derma RM1,000.00 pada 1% bermakna surau menerima RM990.00.\n\n## 6. Kenapa pembayar bayar lebih daripada yang dia taip\n\n![Halaman bayaran pembeli dengan Jumlah kecil, Caj pemprosesan dan Jumlah dibulatkan](06-contoh-pembeli.png)\n\nDerma **RM1,000.00** pada **1%**:\n\n| Baris | Jumlah |\n|---|---|\n| Jumlah kecil (derma) | RM1,000.00 |\n| Caj pemprosesan — FPX 1.00% | RM10.10 |\n| **Jumlah dibayar pembayar** | **RM1,010.10** |\n\nSurau menerima tepat **RM1,000.00**. Sebabnya: CHIP mengira peratusannya pada jumlah yang pembayar **benar-benar bayar**, bukan pada derma yang diminta. Kalau anda hanya menambah 1% pada RM1,000.00 (RM1,010.00), CHIP tetap mengambil RM10.10 dan surau tinggal **RM999.90 — kurang 10 sen**. Jadi JomForm mencari jumlah terkecil yang menutup caj: RM1,010.10.\n\n## 7. Bina borang derma\n\n![Editor produk dengan jenis Bayar ikut suka dan Jumlah minimum RM1.00 dibulatkan](07-borang-derma.png)\n\nForms → **Borang baru** → mod **Borang bayaran**, kemudian tambah produk jenis **Bayar ikut suka (derma)** dengan **Jumlah minimum** RM1.00 — had terendah yang dibenarkan sistem, supaya caj tetap sentiasa dapat ditutup. **Terbitkan** borang, kemudian salin pautan awamnya.\n\n## 8. Uji sendiri dengan RM1.00\n\n![Senarai Jualan dengan jumlah RM1,010.10 dibulatkan](08-jualan.png)\n\nBuat satu derma sebenar RM1.00 dan selesaikan bayaran. Dalam **Jualan**, baris itu menunjukkan jumlah yang pembayar bayar: derma RM1,000.00 muncul sebagai **RM1,010.10**. Baris caj itu sendiri ada pada resit pembeli — **Caj pemprosesan — FPX 1.00%: RM 10.10** — jadi derma surau ialah jumlah bayar tolak caj.\n\n## 9. Edarkan pautan dan kod QR\n\n![Bahagian Kod QR dengan kod QR dan URL borang dibulatkan](09-qr-kongsi.png)\n\nKongsi pautan borang dalam kumpulan WhatsApp surau, atau jana **Kod QR** di Form Settings → **Kod QR (pesanan meja)**, kemudian klik **Muat turun PNG** atau **Cetak stand** untuk papan notis. Jumlah derma harian: **Jualan** → tapis **Tarikh dari** dan **Tarikh hingga**.\n\n## Nota\n\n- Kadar dicatat pada setiap jualan, jadi menukar kadar kemudian tidak mengubah jualan atau resit yang lalu.\n- SST tidak ditambah di atas kadar ini. Kalau acquirer anda menyebut kadar termasuk SST, masukkan angka itu.\n- Halaman bayaran dan resit memaparkan jumlah tanpa pemisah ribu (RM1010.10); senarai Jualan menulis RM1,010.10. Nombornya sama.\n- Penerimaan kad ialah risiko pedagang; pengakuan di langkah 5 direkodkan bersama e-mel dan masanya.\n",
    "shots": [
      {
        "file": "01-mula-percuma.png",
        "width": 1900,
        "height": 1000,
        "detail": {
          "path": "detail/01-mula-percuma.png",
          "width": 820,
          "height": 310
        }
      },
      {
        "file": "02-workspace-surau.png",
        "width": 1900,
        "height": 1000,
        "detail": {
          "path": "detail/02-workspace-surau.png",
          "width": 866,
          "height": 380
        }
      },
      {
        "file": "03-jemput-ahli.png",
        "width": 1900,
        "height": 1000,
        "detail": {
          "path": "detail/03-jemput-ahli.png",
          "width": 690,
          "height": 145
        }
      },
      {
        "file": "04-chip-kunci.png",
        "width": 1900,
        "height": 1000,
        "detail": {
          "path": "detail/04-chip-kunci.png",
          "width": 620,
          "height": 262
        }
      },
      {
        "file": "05-caj-bayaran.png",
        "width": 1900,
        "height": 1000,
        "detail": {
          "path": "detail/05-caj-bayaran.png",
          "width": 866,
          "height": 136
        }
      },
      {
        "file": "06-contoh-pembeli.png",
        "width": 1900,
        "height": 1200,
        "detail": {
          "path": "detail/06-contoh-pembeli.png",
          "width": 600,
          "height": 260
        }
      },
      {
        "file": "07-borang-derma.png",
        "width": 1900,
        "height": 1200,
        "detail": {
          "path": "detail/07-borang-derma.png",
          "width": 710,
          "height": 185
        }
      },
      {
        "file": "08-jualan.png",
        "width": 1900,
        "height": 1200,
        "detail": {
          "path": "detail/08-jualan.png",
          "width": 866,
          "height": 300
        }
      },
      {
        "file": "09-qr-kongsi.png",
        "width": 1900,
        "height": 1200,
        "detail": {
          "path": "detail/09-qr-kongsi.png",
          "width": 866,
          "height": 320
        }
      }
    ],
    "assets": "/panduan/surau"
  }
];

/** The private guide a page renders. Throws at build time rather than shipping a blank page. */
export function privateGuideBySlug(slug: string): PrivateGuide {
  const guide = PRIVATE_GUIDES.find((g) => g.slug === slug);
  if (!guide) {
    throw new Error(
      `no private guide "${slug}" in src/lib/guidesPrivate.gen.ts — run \`npm run guides:sync\`. ` +
        `Known slugs: ${PRIVATE_GUIDES.map((g) => g.slug).join(", ")}`,
    );
  }
  return guide;
}
