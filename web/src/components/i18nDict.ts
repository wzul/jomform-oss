// Locale dictionary for the dashboard UI (issue #514).
//
// This module holds ONLY the dictionary data — deliberately free of React/JSX
// so the locale-completeness guard (src/lib/i18n.test.ts) can import it
// directly with the repo's `npm run test:unit` runner (node --test executes
// TypeScript but cannot load .tsx). The rendering layer, the `t()` lookup and
// the English fallback all live in ../components/i18n.tsx, which imports this
// object and re-exports the `Key` type from it.
//
// Every key MUST define all five locales (see LANGS). `t()` falls back to the
// English string when a locale is absent, so a gap here does not crash — it
// silently shows English to ms/zh/ta/ar users, which is exactly the drift the
// guard exists to prevent.

export const LANGS = ["en", "ms", "zh", "ta", "ar"] as const;
export type Lang = (typeof LANGS)[number];

/**
 * The payment methods the landing hero's demo order form shows under its Pay button
 * (card t_b4e5881d).
 *
 * These are BRAND / SCHEME NAMES, not translated UI chrome: "FPX" is FPX and "DuitNow QR"
 * is DuitNow QR in every locale, so they deliberately are NOT five-locale DICT entries —
 * an entry per locale would invite a translator to "fix" a brand name. What the dictionary
 * DOES own is the claim that these methods are accepted — `land.path2b` and `land.f2`,
 * which are prose and are translated.
 *
 * The list lives here rather than in HeroOrderForm.tsx so that no landing component writes
 * a method name into its own source. The pre-fix hero wrote them down twice — a caption
 * ("Card · FPX · DuitNow QR · GrabPay") and a chip strip below it — and the two copies
 * disagreed ("Card" vs "VISA", four names vs five, different order). One list, read once,
 * is the fix; src/lib/landingPayMethods.test.ts fails if a landing component reintroduces
 * a literal.
 */
export const LANDING_PAY_METHODS = ["FPX", "DuitNow QR", "VISA", "GrabPay", "Boost"] as const;

/**
 * The payment-method radio rows the demo's checkout shows ABOVE its Pay button
 * (card t_453489b9, the approved mockup).
 *
 * The strip of chips that used to sit UNDER the button is gone: the mockup Wan
 * approved asks the buyer how they want to pay before they press Pay, exactly
 * like the real checkout does now, and the Pay button then says which method it
 * will use ("Pay RM186.00 / via FPX"). Keeping both would be the same duplicate
 * statement this list's predecessor was created to delete.
 *
 * The ORDER is the one the product's picker uses (payments.PayMethods in
 * api/internal/payments/paymethod.go): FPX, DuitNow QR, card, e-wallet. Only the
 * dictionary KEYS live here — a method name written in a component's own source
 * is what src/lib/landingPayMethods.test.ts fails on, and a brand name is not
 * translated while the row's description is.
 */
export const LANDING_PAY_OPTIONS = [
  { id: "fpx", labelKey: "land.payfpx", descKey: "land.payfpxdesc" },
  { id: "fpx_b2b1", labelKey: "land.payfpxb2b1", descKey: "land.payfpxb2b1desc" },
  { id: "duitnow_qr", labelKey: "land.paydnqr", descKey: "land.paydnqrdesc" },
  { id: "card", labelKey: "land.paycard", descKey: "land.paycarddesc" },
  { id: "ewallet", labelKey: "land.paywallet", descKey: "land.paywalletdesc" },
] as const;

// The custom-domain CNAME target USED to be a constant here (issue #323). It is
// a deployment fact now — `custom.jomform.com` is the hosted product's target,
// and a self-hosted install points the domain at its own host (card
// t_48a93084). The keys below take `{target}` and every caller fills it from
// lib/deployment.cnameTarget(), so no surface can name a host this install
// does not run.

export const DICT = {
  // nav / shell
  "nav.dashboard": { en: "Dashboard", ms: "Papan Pemuka", zh: "仪表板", ta: "டாஷ்போர்டு", ar: "لوحة التحكم" },
  "nav.forms": { en: "Forms", icon: undefined, ms: "Borang", zh: "表单", ta: "படிவங்கள்", ar: "النماذج" },
  "nav.settings": { en: "Settings", ms: "Tetapan", zh: "设置", ta: "அமைப்புகள்", ar: "الإعدادات" },
  "nav.signout": { en: "Sign out", ms: "Log keluar", zh: "退出登录", ta: "வெளியேறு", ar: "تسجيل الخروج" },
  // Shell chrome only assistive tech reads (card t_b432c9ca): the mobile
  // hamburger, the drawer landmark and its close button. An aria-label is
  // announced in the user's language like any visible label, so it is a
  // dictionary key and not a literal in the component.
  "nav.openMenu": { en: "Open menu", ms: "Buka menu", zh: "打开菜单", ta: "மெனுவைத் திற", ar: "فتح القائمة" },
  "nav.closeMenu": { en: "Close menu", ms: "Tutup menu", zh: "关闭菜单", ta: "மெனுவை மூடு", ar: "إغلاق القائمة" },
  "nav.menu": { en: "Navigation menu", ms: "Menu navigasi", zh: "导航菜单", ta: "வழிசெலுத்தல் மெனு", ar: "قائمة التنقل" },
  // nav group labels (sidebar redesign): the destination list is grouped so a
  // user scanning for one destination reads four labelled sections instead of a
  // flat run of 13 rows. Rendered uppercase by CSS, so the values stay in
  // sentence case like every other label.
  "navgroup.build": { en: "Build", ms: "Bina", zh: "构建", ta: "உருவாக்கு", ar: "الإنشاء" },
  "navgroup.sales": { en: "Sales", ms: "Jualan", zh: "销售", ta: "விற்பனை", ar: "المبيعات" },
  "navgroup.settings": { en: "Settings", ms: "Tetapan", zh: "设置", ta: "அமைப்புகள்", ar: "الإعدادات" },
  "navgroup.help": { en: "Help", ms: "Bantuan", zh: "帮助", ta: "உதவி", ar: "المساعدة" },
  // login
  "login.welcome": { en: "Welcome back", ms: "Selamat kembali", zh: "欢迎回来", ta: "மீண்டும் வருக", ar: "أهلاً بعودتك" },
  "login.registerTitle": { en: "Create your JomForm account", ms: "Cipta akaun JomForm anda", zh: "创建您的 JomForm 账户", ta: "உங்கள் JomForm கணக்கை உருவாக்கவும்", ar: "أنشئ حساب JomForm الخاص بك" },
  "login.tabLogin": { en: "Log in", ms: "Log Masuk", zh: "登录", ta: "உள்நுழைவு", ar: "تسجيل الدخول" },
  "login.tabRegister": { en: "Sign up", ms: "Daftar", zh: "注册", ta: "பதிவு", ar: "إنشاء حساب" },
  "login.email": { en: "Email", ms: "Emel", zh: "电子邮件", ta: "மின்னஞ்சல்", ar: "البريد الإلكتروني" },
  // The field's PLACEHOLDER is a sample address, not a sentence — but it is
  // still text in the merchant's field, so it follows the dashboard language
  // rather than shipping a Malay sample on an English page (t_541a31d5).
  "login.emailPh": { en: "you@yourshop.my", ms: "anda@kedai.my", zh: "you@yourshop.my", ta: "you@yourshop.my", ar: "you@yourshop.my" },
  "login.password": { en: "Password", ms: "Kata laluan", zh: "密码", ta: "கடவுச்சொல்", ar: "كلمة المرور" },
  "login.pwRegister": { en: "Password (optional)", ms: "Kata laluan (pilihan)", zh: "密码（可选）", ta: "கடவுச்சொல் (விருப்பம்)", ar: "كلمة المرور (اختياري)" },
  "login.pwRegisterPh": { en: "Leave blank for now", ms: "Biarkan kosong buat masa ini", zh: "可暂时留空", ta: "இப்போதைக்கு காலியாக விடவும்", ar: "اتركه فارغاً الآن" },
  "login.pwOptional": { en: "Optional. Leave blank to sign up without a password (you can set one later).", ms: "Pilihan — biarkan kosong untuk mendaftar tanpa kata laluan (anda boleh menetapkannya kemudian).", zh: "可选 — 留空可无需密码注册（之后可设置）。", ta: "விருப்பம் — கடவுச்சொல் இல்லாமல் பதிவு செய்ய காலியாக விடவும் (பின்னர் அமைக்கலாம்).", ar: "اختياري — اتركه فارغاً للتسجيل بدون كلمة مرور (يمكنك تعيينها لاحقاً)." },
  "login.submitLogin": { en: "Log in", ms: "Log Masuk", zh: "登录", ta: "உள்நுழை", ar: "تسجيل الدخول" },
  "login.submitRegister": { en: "Create free account", ms: "Cipta Akaun Percuma", zh: "免费创建账户", ta: "இலவச கணக்கை உருவாக்கு", ar: "أنشئ حساباً مجانياً" },
  "login.freeNote": {
    en: "Free forever. Your first form in 5 minutes.",
    ms: "Percuma selamanya — borang pertama siap dalam 5 minit.",
    zh: "永久免费 — 5 分钟创建您的第一个表单。",
    ta: "என்றும் இலவசம் — 5 நிமிடங்களில் உங்கள் முதல் படிவம்.",
    ar: "مجاني للأبد — نموذجك الأول في 5 دقائق.",
  },
  "login.loginNote": { en: "Log in to manage your forms and sales.", ms: "Log masuk untuk urus borang & jualan anda.", zh: "登录以管理您的表单和销售。", ta: "உங்கள் படிவங்களையும் விற்பனையையும் நிர்வகிக்க உள்நுழையவும்.", ar: "سجّل الدخول لإدارة نماذجك ومبيعاتك." },
  // issue #62: remember me + forgot/reset password
  "login.remember": { en: "Remember me", ms: "Ingat saya", zh: "记住我", ta: "என்னை நினைவில் வை", ar: "تذكرني" },
  "login.rememberHint": { en: "Stay signed in on this browser for 14 days", ms: "Kekal log masuk pada pelayar ini selama 14 hari", zh: "在此浏览器上保持登录 14 天", ta: "இந்த உலாவியில் 14 நாட்கள் உள்நுழைந்து இருக்கவும்", ar: "الابقاء على تسجيل الدخول لمدة 14 يوماً" },
  "login.forgot": { en: "Forgot password?", ms: "Lupa kata laluan?", zh: "忘记密码？", ta: "கடவுச்சொல் மறந்துவிட்டதா?", ar: "نسيت كلمة المرور؟" },
  "login.forgotBack": { en: "Back to login", ms: "Kembali ke log masuk", zh: "返回登录", ta: "உள்நுழைவுக்குத் திரும்பு", ar: "العودة إلى تسجيل الدخول" },
  "login.forgotTitle": { en: "Reset your password", ms: "Set semula kata laluan anda", zh: "重置您的密码", ta: "உங்கள் கடவுச்சொல்லை மீட்டமை", ar: "إعادة تعيين كلمة المرور" },
  "login.forgotDesc": { en: "Enter your account email and we'll send you a reset link.", ms: "Masukkan emel akaun anda dan kami akan hantar pautan set semula.", zh: "输入您的账户邮箱，我们会发送重置链接。", ta: "உங்கள் கணக்கு மின்னஞ்சலை உள்ளிட்டால் மீட்டமை இணைப்பு அனுப்புவோம்.", ar: "أدخل بريد حسابك وسنرسل لك رابط إعادة التعيين." },
  "login.forgotSubmit": { en: "Send reset link", ms: "Hantar pautan set semula", zh: "发送重置链接", ta: "மீட்டமை இணைப்பு அனுப்பு", ar: "إرسال رابط إعادة التعيين" },
  "login.forgotSent": { en: "If that email is registered and verified, check your inbox for a reset link.", ms: "Jika emel tersebut berdaftar dan disahkan, semak emel anda untuk pautan set semula.", zh: "如果该邮箱已注册并验证，请查收邮件中的重置链接。", ta: "அந்த மின்னஞ்சல் பதிவுசெய்யப்பட்டு சரிபார்க்கப்பட்டால், மீட்டமை இணைப்புக்காக உங்கள் மின்னஞ்சலைச் சரிபார்க்கவும்.", ar: "إذا كان البريد مسجلاً وموثقاً، تحقق من بريدك للحصول على رابط إعادة التعيين." },
  "reset.title": { en: "Set a new password", ms: "Tetapkan kata laluan baharu", zh: "设置新密码", ta: "புதிய கடவுச்சொல்லை அமை", ar: "تعيين كلمة مرور جديدة" },
  "reset.desc": { en: "Choose a new password for your account.", ms: "Pilih kata laluan baharu untuk akaun anda.", zh: "为您的账户选择新密码。", ta: "உங்கள் கணக்கிற்கு புதிய கடவுச்சொல்லைத் தேர்வுசெய்யவும்.", ar: "اختر كلمة مرور جديدة لحسابك." },
  "reset.newPw": { en: "New password (min. 10 chars)", ms: "Kata laluan baharu (min. 10 aksara)", zh: "新密码（至少 10 个字符）", ta: "புதிய கடவுச்சொல் (குறைந்தது 10 எழுத்துகள்)", ar: "كلمة المرور الجديدة (10 أحرف على الأقل)" },
  "reset.confirmPw": { en: "Confirm new password", ms: "Sahkan kata laluan baharu", zh: "确认新密码", ta: "புதிய கடவுச்சொல்லை உறுதிப்படுத்து", ar: "تأكيد كلمة المرور الجديدة" },
  "reset.submit": { en: "Reset password", ms: "Set semula kata laluan", zh: "重置密码", ta: "கடவுச்சொல்லை மீட்டமை", ar: "إعادة تعيين كلمة المرور" },
  "reset.done": { en: "Password reset! Please log in.", ms: "Kata laluan diset semula! Sila log masuk.", zh: "密码已重置！请登录。", ta: "கடவுச்சொல் மீட்டமைக்கப்பட்டது! உள்நுழைக.", ar: "تمت إعادة تعيين كلمة المرور! يرجى تسجيل الدخول." },
  "reset.invalid": { en: "This reset link is invalid or has expired. Request a new one from the sign-in page.", ms: "Pautan set semula ini tidak sah atau telah tamat tempoh. Minta pautan baharu dari halaman log masuk.", zh: "此重置链接无效或已过期。请在登录页面重新申请。", ta: "இந்த மீட்டமை இணைப்பு தவறானது அல்லது காலாவதியானது. உள்நுழைவுப் பக்கத்திலிருந்து புதிய இணைப்பைக் கோருங்கள்.", ar: "رابط إعادة التعيين هذا غير صالح أو منتهي الصلاحية. اطلب رابطاً جديداً من صفحة تسجيل الدخول." },
  "reset.pwMismatch": { en: "Passwords don't match", ms: "Kata laluan tidak sepadan", zh: "两次输入的密码不一致", ta: "கடவுச்சொற்கள் பொருந்தவில்லை", ar: "كلمتا المرور غير متطابقتين" },
  // issue #63: trusted device
  "login.totpRemember": { en: "Don't ask for 2FA code on this device for 30 days", ms: "Jangan tanya kod 2FA pada peranti ini selama 30 hari", zh: "30 天内不在本设备请求 2FA 验证码", ta: "30 நாட்கள் இந்த சாதனத்தில் 2FA குறியீட்டைக் கேட்க வேண்டாம்", ar: "لا تطلب رمز التحقق الثنائي على هذا الجهاز لمدة 30 يوماً" },
  // issue #71: two-step 2FA login (step 2 is code-only)
  "login.totpTitle": { en: "Enter 2FA code", ms: "Masukkan kod 2FA", zh: "输入 2FA 验证码", ta: "2FA குறியீட்டை உள்ளிடவும்", ar: "أدخل رمز التحقق الثنائي" },
  "login.totpDesc": { en: "Open your authenticator app and enter the 6-digit code for {email}.", ms: "Buka aplikasi pengesah anda dan masukkan kod 6 digit untuk {email}.", zh: "打开验证器应用，为 {email} 输入 6 位验证码。", ta: "உங்கள் அங்கீகாரப் பயன்பாட்டைத் திறந்து, {email} க்கான 6 இலக்க குறியீட்டை உள்ளிடவும்.", ar: "افتح تطبيق المصادقة وأدخل الرمز المكوّن من 6 أرقام لـ {email}." },
  "login.totpSubmit": { en: "Verify & sign in", ms: "Sahkan & log masuk", zh: "验证并登录", ta: "சரிபார்த்து உள்நுழை", ar: "تحقق وسجّل الدخول" },
  // Card t_adf8e1e6: the 2FA step's OWN failure sentence. The server answers a
  // bad CODE with the same deliberately uniform 401 + `login.invalid` it uses for
  // a bad password (no-enumeration), so the step-2 surface has to re-map that one
  // code to a sentence about the code — "Email or password is incorrect." tells a
  // user who was proven correct one screen earlier to doubt a credential that was
  // not the problem, and never mentions expiry or re-requesting.
  "login.totpInvalid": { en: "That code is invalid or expired. Request a new one.", ms: "Kod itu tidak sah atau telah tamat tempoh. Minta kod baharu.", zh: "该验证码无效或已过期。请重新获取。", ta: "அந்தக் குறியீடு தவறானது அல்லது காலாவதியானது. புதிய குறியீட்டைக் கோருங்கள்.", ar: "رمز التحقق هذا غير صالح أو منتهي الصلاحية. اطلب رمزاً جديداً." },
  "login.totpBack": { en: "Change email / use another account", ms: "Ganti emel / Guna akaun lain", zh: "更换邮箱 / 使用其他账户", ta: "மின்னஞ்சலை மாற்று / மற்றொரு கணக்கைப் பயன்படுத்து", ar: "تغيير البريد الإلكتروني / استخدام حساب آخر" },
  // issue #72: 2FA email one-time backup code
  "login.emailCodeLink": { en: "No authenticator app access? Send a code via email", ms: "Tak ada akses aplikasi pengesah? Hantar kod melalui emel", zh: "没有验证器应用？通过电子邮件发送验证码", ta: "அங்கீகாரப் பயன்பாட்டு அணுகல் இல்லையா? மின்னஞ்சல் வழியாக குறியீடு அனுப்பு", ar: "لا يمكن الوصول إلى تطبيق المصادقة؟ أرسل رمزاً عبر البريد الإلكتروني" },
  "login.emailCodeSent": { en: "If eligible, a 6-digit backup code has been emailed to you. Enter it below.", ms: "Jika layak, kod sandaran 6-angka telah dihantar ke emel anda. Masukkan di bawah.", zh: "如果符合条件，6 位备份验证码已发送至您的邮箱。请输入。", ta: "தகுதி பெற்றால், 6-இலக்க காப்பு குறியீடு உங்கள் மின்னஞ்சலுக்கு அனுப்பப்பட்டுள்ளது. கீழே உள்ளிடவும்.", ar: "إذا كنت مؤهلاً، فقد تم إرسال رمز نسخ احتياطي من 6 أرقام إلى بريدك. أدخله أدناه." },
  // issue #73: passwordless magic link
  "login.passwordless": { en: "Log in without password", ms: "Log masuk tanpa kata laluan", zh: "无需密码登录", ta: "கடவுச்சொல் இல்லாமல் உள்நுழை", ar: "تسجيل الدخول بدون كلمة مرور" },
  // Card t_df9233f6: the passkey sign-in control on the login page. This key
  // had NO dictionary entry while the button was unreachable; the button and
  // the string ship together.
  "login.passkey": { en: "Sign in with a passkey", ms: "Log masuk dengan passkey", zh: "使用通行密钥登录", ta: "கடவுச்சாவி மூலம் உள்நுழை", ar: "سجّل الدخول بمفتاح المرور" },
  "login.passwordlessBack": { en: "Back to sign-in", ms: "Kembali ke log masuk", zh: "返回登录", ta: "உள்நுழைவுக்குத் திரும்பு", ar: "العودة إلى تسجيل الدخول" },
  "login.passwordlessDesc": { en: "Enter your email and we'll email you a sign-in link. New to JomForm? Clicking it creates your account and signs you in.", ms: "Masukkan emel anda dan kami akan hantar pautan log masuk. Baru di JomForm? Mengkliknya akan mencipta akaun anda dan log masuk.", zh: "输入您的邮箱，我们会向您发送登录链接。JomForm 新手？点击它将创建您的账户并登录。", ta: "உங்கள் மின்னஞ்சலை உள்ளிட்டால் உள்நுழைவு இணைப்பு அனுப்புவோம். JomForm இல் புதியவரா? கிளிக் செய்தால் உங்கள் கணக்கு உருவாக்கப்பட்டு உள்நுழையும்.", ar: "أدخل بريدك وسنرسل لك رابط تسجيل الدخول. جديد على JomForm؟ سيؤدي النقر عليه إلى إنشاء حسابك وتسجيل الدخول." },
  "login.passwordlessSubmit": { en: "Email me a sign-in link", ms: "Hantar pautan log masuk ke emel saya", zh: "给我发送登录链接", ta: "எனக்கு உள்நுழைவு இணைப்பு அனுப்பு", ar: "أرسل لي رابط تسجيل الدخول" },
  // issue #271: passwordless (magic link) is the default login; password is
  // the secondary option.
  "login.loginWithPassword": { en: "Login with password", ms: "Log masuk dengan kata laluan", zh: "使用密码登录", ta: "கடவுச்சொல்லுடன் உள்நுழை", ar: "تسجيل الدخول بكلمة المرور" },
  "login.passwordlessSent": { en: "If that email is registered, a sign-in link is on its way. New emails get an account created on click. Check your inbox.", ms: "Jika emel tersebut berdaftar, pautan log masuk telah dihantar. Emel baharu akan mencipta akaun apabila diklik. Semak peti masuk anda.", zh: "如果该邮箱已注册，登录链接正在路上。新邮箱点击后将创建账户。请查收邮件。", ta: "அந்த மின்னஞ்சல் பதிவுசெய்யப்பட்டால், உள்நுழைவு இணைப்பு அனுப்பப்படும். புதிய மின்னஞ்சல்கள் கிளிக் செய்தவுடன் கணக்கை உருவாக்கும். உங்கள் இன்பாக்ஸைச் சரிபார்க்கவும்.", ar: "إذا كان البريد مسجلاً، فسيصل رابط تسجيل الدخول. البريد الجديد ينشئ حساباً عند النقر. تحقق من بريدك." },
  // issue #304: signup via magic link is discoverable directly on the login page
  "login.newHereSignup": { en: "New here? Sign up", ms: "Baru di sini? Daftar", zh: "新用户？注册", ta: "புதியவரா? பதிவு செய்க", ar: "جديد هنا؟ سجّل" },
  "login.signupHere": { en: "Enter your email and we will email you a sign-in link. The first time signs you up.", ms: "Masukkan emel anda dan kami akan hantar pautan log masuk — kali pertama akan mendaftar akaun anda.", zh: "输入您的邮箱，我们会向您发送登录链接——首次使用将为您创建账户。", ta: "உங்கள் மின்னஞ்சலை உள்ளிட்டால் உள்நுழைவு இணைப்பு அனுப்புவோம் — முதல் முறை உங்கள் கணக்கை உருவாக்கும்.", ar: "أدخل بريدك وسنرسل لك رابط تسجيل الدخول — المرة الأولى تنشئ حسابك." },
  "login.signupHint": { en: "Clicking the email link creates your JomForm account and signs you in.", ms: "Mengklik pautan emel akan mencipta akaun JomForm anda dan log masuk.", zh: "点击邮箱链接将创建您的 JomForm 账户并登录。", ta: "மின்னஞ்சல் இணைப்பைக் கிளிக் செய்தால் உங்கள் JomForm கணக்கு உருவாக்கப்பட்டு உள்நுழையும்.", ar: "سيؤدي النقر على رابط البريد إلى إنشاء حساب JomForm الخاص بك وتسجيل دخولك." },
  "login.orSignupWithPassword": { en: "or register with a password", ms: "atau daftar dengan kata laluan", zh: "或使用密码注册", ta: "அல்லது கடவுச்சொல்லுடன் பதிவு செய்க", ar: "أو سجّل بكلمة مرور" },
  // issue #313: single email-first screen (login + signup via one magic-link
  // field, Notion/Slack/Linear pattern). Password and social stay secondary.
  "login.emailFirstTitle": { en: "Sign in or sign up", ms: "Log masuk atau daftar", zh: "登录或注册", ta: "உள்நுழை அல்லது பதிவு செய்க", ar: "سجّل الدخول أو أنشئ حساباً" },
  "login.emailFirstDesc": { en: "Enter your email and we'll email you a magic sign-in link. New to JomForm? Clicking it creates your account and signs you in.", ms: "Masukkan emel anda dan kami akan hantar pautan log masuk ajaib. Baru di JomForm? Mengkliknya akan mencipta akaun anda dan log masuk.", zh: "输入您的邮箱，我们会向您发送魔法登录链接。JomForm 新手？点击它将创建您的账户并登录。", ta: "உங்கள் மின்னஞ்சலை உள்ளிட்டால் மேஜிக் உள்நுழைவு இணைப்பு அனுப்புவோம். JomForm இல் புதியவரா? கிளிக் செய்தால் உங்கள் கணக்கு உருவாக்கப்பட்டு உள்நுழையும்.", ar: "أدخل بريدك وسنرسل لك رابط تسجيل دخول سحرياً. جديد على JomForm؟ سيؤدي النقر عليه إلى إنشاء حسابك وتسجيل الدخول." },
  "login.continue": { en: "Continue", ms: "Teruskan", zh: "继续", ta: "தொடரவும்", ar: "متابعة" },
  "login.newToJomform": { en: "New to JomForm? You will get a magic link. No password needed to start.", ms: "Baru di JomForm? Anda akan menerima pautan ajaib — tiada kata laluan diperlukan untuk bermula.", zh: "JomForm 新手？您将收到一个魔法链接——无需密码即可开始。", ta: "JomForm இல் புதியவரா? மேஜிக் இணைப்பு கிடைக்கும் — தொடங்க கடவுச்சொல் தேவையில்லை.", ar: "جديد على JomForm؟ ستصلك رسالة سحرية — لا حاجة لكلمة مرور للبدء." },
  // issue #114: remaining login UI chrome that was hardcoded BM
  "login.registerDone": { en: "Your account has been created! Please verify your email address via the link we sent to activate your login.", ms: "Akaun anda telah dicipta! Sila sahkan alamat emel anda melalui pautan yang kami hantar untuk mengaktifkan log masuk.", zh: "您的账户已创建！请通过我们发送的链接验证您的邮箱地址以激活登录。", ta: "உங்கள் கணக்கு உருவாக்கப்பட்டது! உள்நுழைவைச் செயல்படுத்த நாங்கள் அனுப்பிய இணைப்பு மூலம் உங்கள் மின்னஞ்சல் முகவரியைச் சரிபார்க்கவும்.", ar: "تم إنشاء حسابك! يرجى التحقق من عنوان بريدك الإلكتروني عبر الرابط الذي أرسلناه لتفعيل تسجيل الدخول." },
  "login.loginDone": { en: "Logged in successfully.", ms: "Berjaya log masuk.", zh: "登录成功。", ta: "வெற்றிகரமாக உள்நுழைந்தீர்கள்.", ar: "تم تسجيل الدخول بنجاح." },
  "login.backToLogin": { en: "Back to login", ms: "Ke Log Masuk", zh: "返回登录", ta: "உள்நுழைவுக்குத் திரும்பு", ar: "العودة إلى تسجيل الدخول" },
  "login.honeypot": { en: "Website", ms: "Laman web", zh: "网站", ta: "இணையதளம்", ar: "الموقع" },
  "login.genericError": { en: "Failed. Please try again.", ms: "Gagal. Cuba semula.", zh: "失败。请重试。", ta: "தோல்வி. மீண்டும் முயற்சிக்கவும்.", ar: "فشل. يرجى المحاولة مرة أخرى." },
  "login.passkeyInvalid": { en: "That passkey did not match. Try again, or sign in with your password.", ms: "Passkey itu tidak sepadan. Cuba lagi, atau log masuk dengan kata laluan anda.", zh: "该通行密钥不匹配。请重试，或使用密码登录。", ta: "அந்தக் கடவுச்சாவி பொருந்தவில்லை. மீண்டும் முயலவும் அல்லது கடவுச்சொல்லுடன் உள்நுழையவும்.", ar: "مفتاح المرور هذا غير مطابق. أعد المحاولة أو سجّل الدخول بكلمة المرور." },
  "login.passkeyUnsupported": { en: "Passkey cancelled or not supported by this browser.", ms: "Passkey dibatalkan atau tidak disokong pelayar.", zh: "通行密钥已取消或此浏览器不支持。", ta: "கடவுச்சாவி ரத்துசெய்யப்பட்டது அல்லது இந்த உலாவி ஆதரிக்கவில்லை.", ar: "تم إلغاء مفتاح المرور أو المتصفح لا يدعمه." },
  "login.continueWith": { en: "Continue with {provider}", ms: "Teruskan dengan {provider}", zh: "使用 {provider} 继续", ta: "{provider} உடன் தொடரவும்", ar: "المتابعة عبر {provider}" },
  "login.termsNote": { en: "By continuing, you agree to JomForm's Terms & Privacy Policy.", ms: "Dengan meneruskan, anda bersetuju dengan Terma & Polisi Privasi JomForm.", zh: "继续即表示您同意 JomForm 的服务条款和隐私政策。", ta: "தொடர்வதன் மூலம், JomForm இன் விதிமுறைகள் & தனியுரிமைக் கொள்கையை நீங்கள் ஏற்கிறீர்கள்.", ar: "بالمتابعة، فإنك توافق على شروط وسياسة خصوصية JomForm." },
  // issue #436: back-to-home link on the login page
  "login.backToHome": { en: "← Back to home", ms: "← Kembali ke laman utama", zh: "← 返回首页", ta: "← முகப்புக்குத் திரும்பு", ar: "← العودة إلى الصفحة الرئيسية" },
  "magic.title": { en: "Signing you in…", ms: "Memproses log masuk…", zh: "正在登录…", ta: "உள்நுழைக்கிறது…", ar: "جارٍ تسجيل الدخول…" },
  "magic.invalid": { en: "This sign-in link is invalid or has expired. Go back and request a new one.", ms: "Pautan log masuk ini tidak sah atau telah tamat tempoh. Kembali dan minta pautan baharu.", zh: "此登录链接无效或已过期。请返回并重新申请。", ta: "இந்த உள்நுழைவு இணைப்பு தவறானது அல்லது காலாவதியானது. திரும்பிச் சென்று புதிய இணைப்பைக் கோருங்கள்.", ar: "رابط تسجيل الدخول هذا غير صالح أو منتهي الصلاحية. ارجع واطلب رابطاً جديداً." },
  "magic.goLogin": { en: "Go to sign in", ms: "Ke log masuk", zh: "前往登录", ta: "உள்நுழைவுக்குச் செல்", ar: "الانتقال إلى تسجيل الدخول" },
  // issue #302: explicit social account connect
  "connect.title": { en: "Connect your account", ms: "Hubungkan akaun anda", zh: "关联您的账户", ta: "உங்கள் கணக்கை இணைக்கவும்", ar: "ربط حسابك" },
  "connect.verifying": { en: "Verifying your ownership link…", ms: "Mengesahkan pautan pemilikan anda…", zh: "正在验证您的所有权链接…", ta: "உங்கள் உரிமை இணைப்பைச் சரிபார்க்கிறது…", ar: "جارٍ التحقق من رابط الملكية…" },
  "connect.success": { en: "Account connected. You're now signed in.", ms: "Akaun dihubungkan. Anda kini log masuk.", zh: "账户已关联。您现在已登录。", ta: "கணக்கு இணைக்கப்பட்டது. நீங்கள் இப்போது உள்நுழைந்துள்ளீர்கள்.", ar: "تم ربط الحساب. أنت الآن مسجل الدخول." },
  "connect.explain": { en: "This social account matches an existing JomForm account. We emailed you a verification link. Open it to confirm you own this account before we connect it.", ms: "Akaun sosial ini sepadan dengan akaun JomForm sedia ada. Kami menghantar pautan pengesahan kepada anda — buka untuk mengesahkan anda memiliki akaun ini sebelum dihubungkan.", zh: "该社交账户与现有 JomForm 账户匹配。我们已向您发送验证链接——请打开确认您拥有该账户后再关联。", ta: "இந்த சமூக கணக்கு ஏற்கனவே உள்ள JomForm கணக்குடன் பொருந்துகிறது. இது இணைக்கப்படுவதற்கு முன் நீங்கள் இந்த கணக்கை வைத்திருக்கிறீர்கள் என்பதை உறுதிப்படுத்த ஒப்பளிப்பு இணைப்பை உங்களுக்கு அனுப்பியுள்ளோம்.", ar: "يتطابق هذا الحساب الاجتماعي مع حساب JomForm موجود. أرسلنا إليك رابط تحقق — افتحه لتأكيد ملكيتك لهذا الحساب قبل ربطه." },
  "connect.explainShort": { en: "This social account matches an existing JomForm account. Check your inbox for the verification link we emailed you.", ms: "Akaun sosial ini sepadan dengan akaun JomForm sedia ada. Periksa peti masuk anda untuk pautan pengesahan yang kami hantar.", zh: "该社交账户与现有 JomForm 账户匹配。请查收我们发送的验证邮件。", ta: "இந்த சமூக கணக்கு ஏற்கனவே உள்ள JomForm கணக்குடன் பொருந்துகிறது. நாங்கள் அனுப்பிய ஒப்பளிப்பு இணைப்புக்காக உங்கள் இன்பாக்ஸை சரிபார்க்கவும்.", ar: "يتطابق هذا الحساب الاجتماعي مع حساب JomForm موجود. تحقق من بريدك الوارد للحصول على رابط التحقق الذي أرسلناه." },
  "connect.resend": { en: "Send verification email again", ms: "Hantar semula emel pengesahan", zh: "重新发送验证邮件", ta: "ஒப்பளிப்பு மின்னஞ்சலை மீண்டும் அனுப்பு", ar: "إعادة إرسال بريد التحقق" },
  "connect.resendSent": { en: "Verification email sent. Check your inbox and open the link to finish connecting.", ms: "Emel pengesahan dihantar. Periksa peti masuk anda dan buka pautan untuk selesai menghubungkan.", zh: "验证邮件已发送。请查收并打开链接完成关联。", ta: "ஒப்பளிப்பு மின்னஞ்சல் அனுப்பப்பட்டது. உங்கள் இன்பாக்ஸைச் சரிபார்த்து, இணைப்பை முடிக்க இணைப்பைத் திறக்கவும்.", ar: "تم إرسال بريد التحقق. تحقق من بريدك الوارد وافتح الرابط لإنهاء الربط." },
  "connect.stale": { en: "This link is incomplete or has expired. Please sign in again with your social account.", ms: "Pautan ini tidak lengkap atau telah tamat tempoh. Sila log masuk semula dengan akaun sosial anda.", zh: "此链接不完整或已过期。请重新使用社交账户登录。", ta: "இந்த இணைப்பு முழுமையடையவில்லை அல்லது காலாவதியானது. உங்கள் சமூக கணக்கில் மீண்டும் உள்நுழையவும்.", ar: "هذا الرابط غير مكتمل أو منتهي الصلاحية. يرجى تسجيل الدخول مرة أخرى بحسابك الاجتماعي." },
  // The page's OWN sentences for a failure it cannot explain by code: an
  // unknown code, or a body that is not JSON at all. Before these existed the
  // /connect page had two hardcoded English strings here (issue #6177 follow-up),
  // so this surface stayed English for a user whose dashboard is Malay.
  "connect.fail": { en: "We couldn't connect this account. Please sign in again with your social account.", ms: "Kami tidak dapat menghubungkan akaun ini. Sila log masuk semula dengan akaun sosial anda.", zh: "无法关联此账户。请重新使用您的社交账户登录。", ta: "இந்தக் கணக்கை இணைக்க முடியவில்லை. உங்கள் சமூக கணக்கில் மீண்டும் உள்நுழையவும்.", ar: "تعذّر ربط هذا الحساب. يرجى تسجيل الدخول مرة أخرى بحسابك الاجتماعي." },
  "connect.resendFail": { en: "We couldn't send the verification email. Please try again.", ms: "Kami tidak dapat menghantar emel pengesahan. Sila cuba lagi.", zh: "无法发送验证邮件。请重试。", ta: "ஒப்பளிப்பு மின்னஞ்சலை அனுப்ப முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر إرسال بريد التحقق. يرجى المحاولة مرة أخرى." },
  "prof.trusted": { en: "2FA active · device trusted until {date}", ms: "2FA aktif · peranti dipercayai sehingga {date}", zh: "2FA 已启用 · 设备信任至 {date}", ta: "2FA செயலில் · சாதனம் {date} வரை நம்பப்படுகிறது", ar: "2FA نشط · الجهاز موثوق حتى {date}" },
  "prof.forgetDevice": { en: "Forget this device", ms: "Lupa peranti ini", zh: "忘记此设备", ta: "இந்த சாதனத்தை மற", ar: "نسيان هذا الجهاز" },
  // issue #454: trusted devices management list + revoke
  "prof.trustedDevices": { en: "Trusted devices", ms: "Peranti dipercayai", zh: "受信任的设备", ta: "நம்பகமான சாதனங்கள்", ar: "الأجهزة الموثوقة" },
  "prof.trustedDevicesHelp": { en: "Devices that can skip the 2FA code for 30 days. Revoke any device you no longer use. The next login there will ask for a code again.", ms: "Peranti yang boleh melangkau kod 2FA selama 30 hari. Batalkan mana-mana peranti yang tidak lagi digunakan — log masuk seterusnya akan meminta kod semula.", zh: "这些设备可在 30 天内跳过 2FA 验证码。撤销您不再使用的设备 — 该设备下次登录将重新要求验证码。", ta: "30 நாட்களுக்கு 2FA குறியீட்டைத் தவிர்க்கக்கூடிய சாதனங்கள். இனி பயன்படுத்தாத சாதனத்தை ரத்துசெய்யவும் — அடுத்த உள்நுழைவில் மீண்டும் குறியீடு கேட்கப்படும்.", ar: "الأجهزة التي يمكنها تخطي رمز التحقق الثنائي لمدة 30 يوماً. ألغِ أي جهاز لم تعد تستخدمه — سيسأل تسجيل الدخول التالي عن الرمز مجدداً." },
  "prof.trustedDevicesEmpty": { en: "No trusted devices yet.", ms: "Belum ada peranti dipercayai.", zh: "尚无受信任的设备。", ta: "இன்னும் நம்பகமான சாதனங்கள் இல்லை.", ar: "لا توجد أجهزة موثوقة بعد." },
  "prof.deviceThis": { en: "This device", ms: "Peranti ini", zh: "本设备", ta: "இந்த சாதனம்", ar: "هذا الجهاز" },
  "prof.deviceUnknown": { en: "Unknown device", ms: "Peranti tidak dikenali", zh: "未知设备", ta: "தெரியாத சாதனம்", ar: "جهاز غير معروف" },
  "prof.deviceIP": { en: "IP", ms: "IP", zh: "IP", ta: "IP", ar: "IP" },
  "prof.deviceFirstSeen": { en: "First trusted", ms: "Mula dipercayai", zh: "首次信任", ta: "முதல் நம்பிக்கை", ar: "أول ثقة" },
  "prof.deviceExpires": { en: "Expires", ms: "Tamat tempoh", zh: "到期", ta: "காலாவதி", ar: "ينتهي" },
  "prof.revoke": { en: "Revoke", ms: "Batalkan", zh: "撤销", ta: "ரத்துசெய்", ar: "إلغاء" },
  "prof.revokeDeviceConfirm": { en: "Revoke this trusted device? Its next login will require a 2FA code again.", ms: "Batalkan peranti dipercayai ini? Log masuk seterusnya akan meminta kod 2FA semula.", zh: "撤销此受信任设备？该设备下次登录将重新要求 2FA 验证码。", ta: "இந்த நம்பகமான சாதனத்தை ரத்துசெய்யவா? அடுத்த உள்நுழைவில் மீண்டும் 2FA குறியீடு தேவைப்படும்.", ar: "إلغاء هذا الجهاز الموثوق؟ سيتطلب تسجيل الدخول التالي رمز تحقق ثنائي مجدداً." },
  "prof.revokeAll": { en: "Revoke all / log out everywhere", ms: "Batalkan semua / log keluar di semua tempat", zh: "全部撤销 / 在所有位置退出登录", ta: "அனைத்தையும் ரத்துசெய் / எல்லா இடங்களிலும் வெளியேறு", ar: "إلغاء الكل / تسجيل الخروج من كل مكان" },
  "prof.revokeAllConfirm": { en: "Revoke every trusted device? Every device will need a 2FA code at its next login.", ms: "Batalkan semua peranti dipercayai? Setiap peranti akan memerlukan kod 2FA pada log masuk seterusnya.", zh: "撤销所有受信任设备？每台设备下次登录都需要 2FA 验证码。", ta: "அனைத்து நம்பகமான சாதனங்களையும் ரத்துசெய்யவா? ஒவ்வொரு சாதனமும் அடுத்த உள்நுழைவில் 2FA குறியீட்டைக் கேட்கும்.", ar: "إلغاء كل الأجهزة الموثوقة؟ سيحتاج كل جهاز إلى رمز تحقق ثنائي عند تسجيل الدخول التالي." },
  "prof.revoked": { en: "Revoked ✓", ms: "Dibatalkan ✓", zh: "已撤销 ✓", ta: "ரத்துசெய்யப்பட்டது ✓", ar: "تم الإلغاء ✓" },
  // dashboard
  "dash.title": { en: "Dashboard", ms: "Papan Pemuka", zh: "仪表板", ta: "டாஷ்போர்டு", ar: "لوحة التحكم" },
  "dash.subtitle": { en: "Overview of your forms and sales", ms: "Ringkasan borang & jualan anda", zh: "表单与销售概览", ta: "படிவங்கள் மற்றும் விற்பனை கண்ணோட்டம்", ar: "نظرة عامة على النماذج والمبيعات" },
  "dash.newForm": { en: "New form", ms: "+ Borang Baru", zh: "+ 新建表单", ta: "+ புதிய படிவம்", ar: "+ نموذج جديد" },
  "dash.revenue": { en: "Confirmed revenue", ms: "Hasil disahkan", zh: "已确认收入", ta: "உறுதிப்படுத்தப்பட்ட வருவாய்", ar: "الإيرادات المؤكدة" },
  "dash.pending": { en: "Pending", ms: "Menunggu", zh: "待定", ta: "நிலுவையில்", ar: "قيد الانتظار" },
  "dash.confirmed": { en: "Confirmed", ms: "Disahkan", zh: "已确认", ta: "உறுதிப்படுத்தப்பட்டது", ar: "مؤكد" },
  "dash.activeProducts": { en: "Active products", ms: "Produk aktif", zh: "在售商品", ta: "செயலில் உள்ள தயாரிப்புகள்", ar: "المنتجات النشطة" },
  "dash.recentSales": { en: "Recent sales", ms: "Jualan terkini", zh: "最新销售", ta: "சமீபத்திய விற்பனை", ar: "أحدث المبيعات" },
  "dash.records": { en: "records", ms: "rekod", zh: "条记录", ta: "பதிவுகள்", ar: "سجلات" },
  "dash.ref": { en: "Reference", ms: "Rujukan", zh: "参考号", ta: "குறிப்பு", ar: "المرجع" },
  "dash.buyer": { en: "Buyer", ms: "Pembeli", zh: "买家", ta: "வாங்குபவர்", ar: "المشتري" },
  "dash.amount": { en: "Amount", ms: "Jumlah", zh: "金额", ta: "தொகை", ar: "المبلغ" },
  "dash.status": { en: "Status", ms: "Status", zh: "状态", ta: "நிலை", ar: "الحالة" },
  "dash.emptySales": { en: "No sales yet.", ms: "Belum ada jualan lagi.", zh: "暂无销售。", ta: "இன்னும் விற்பனை இல்லை.", ar: "لا توجد مبيعات بعد." },
  "dash.formAnalytics": { en: "Form analytics", ms: "Analitik borang", zh: "表单分析", ta: "படிவ பகுப்பாய்வு", ar: "تحليلات النماذج" },
  // AGENTS.md documentation-parity rule 4: the inline "?" doc link next to the
  // per-form counter (issue #598).
  "dash.formAnalyticsDocs": { en: "What the per-form counters mean", ms: "Maksud pembilang per-borang", zh: "每个表单计数的含义", ta: "ஒரு படிவத்தின் எண்ணிக்கையின் பொருள்", ar: "معنى عدّادات كل نموذج" },
  "dash.productPerf": { en: "Product performance", ms: "Prestasi produk", zh: "商品表现", ta: "பொருள் செயல்திறன்", ar: "أداء المنتجات" },
  "dash.submissions": { en: "Submissions:", ms: "Submisi:", zh: "提交数：", ta: "சமர்ப்பிப்புகள்:", ar: "الإرسالات:" },
  // Issue #598: the per-form counter for a PAYMENT form counts its sales rows
  // (paid + failed + pending + refunded), not submission rows — calling that
  // "Submissions:" was the reported defect. The label follows the mode:
  // response → dash.submissions, payment → this key.
  "dash.entries": { en: "Entries:", ms: "Entri:", zh: "条目：", ta: "பதிவுகள்:", ar: "الإدخالات:" },
  "dash.paidConfirmed": { en: "Paid+Confirmed:", ms: "Bayar+Disahkan:", zh: "已付+已确认：", ta: "செலுத்திய+உறுதிப்படுத்தப்பட்ட:", ar: "مدفوع+مؤكد:" },
  "dash.pendingLabel": { en: "Pending:", ms: "Menunggu:", zh: "待定：", ta: "நிலுவை:", ar: "قيد الانتظار:" },
  "dash.failedLabel": { en: "Failed:", ms: "Gagal:", zh: "失败：", ta: "தோல்வி:", ar: "فاشل:" },
  "dash.noForms": { en: "No forms yet.", ms: "Belum ada borang.", zh: "暂无表单。", ta: "இன்னும் படிவங்கள் இல்லை.", ar: "لا نماذج بعد." },
  "dash.sold": { en: "sold", ms: "terjual", zh: "已售", ta: "விற்றது", ar: "مبيع" },
  "dash.paid": { en: "paid", ms: "dibayar", zh: "已付款", ta: "செலுத்தியது", ar: "مدفوع" },
  "dash.gross": { en: "gross", ms: "kasar", zh: "总额", ta: "மொத்தம்", ar: "إجمالي" },
  // Card t_8064a965: the dashboard's revenue figures headline what the merchant
  // KEEPS, so the collected amount and the buyer-borne fee behind it are named
  // beside the figure. `dash.fee` uses the receipt's own word for the charge
  // (payments.FeeLine / httpapi formLang FeeLine) so one charge is never called
  // two things; the five strings are pinned against that map in
  // src/lib/dashboardFee.test.ts.
  "dash.collected": { en: "Buyers paid", ms: "Pembeli bayar", zh: "买家支付", ta: "வாங்குபவர் செலுத்தியது", ar: "دفع المشترون" },
  "dash.fee": { en: "Platform fee", ms: "Caj platform", zh: "平台费", ta: "தளக் கட்டணம்", ar: "رسوم المنصة" },
  "dash.revenueDocs": { en: "What this revenue figure counts", ms: "Apakah yang dikira oleh hasil ini", zh: "此收入数字包含什么", ta: "இந்த வருவாய் எதைக் கணக்கிடுகிறது", ar: "ما الذي تحسبه إيراداتك" },
  "dash.noProdSales": { en: "No product sales yet.", ms: "Belum ada jualan produk.", zh: "暂无商品销售。", ta: "இன்னும் பொருள் விற்பனை இல்லை.", ar: "لا مبيعات منتجات بعد." },
  "dash.pageOf": { en: "page {page} of {total}", ms: "halaman {page} daripada {total}", zh: "第 {page} 页，共 {total} 页", ta: "பக்கம் {page} / {total}", ar: "صفحة {page} من {total}" },
  // issue #106: per-sale receipt email status (Sent/Delivered/Opened/Clicked)
  "dash.email": { en: "Email", ms: "Emel", zh: "邮件", ta: "மின்னஞ்சல்", ar: "البريد" },
  "dash.emailSent": { en: "Sent", ms: "Dihantar", zh: "已发送", ta: "அனுப்பப்பட்டது", ar: "أُرسل" },
  "dash.emailDelivered": { en: "Delivered", ms: "Dihantar", zh: "已送达", ta: "வழங்கப்பட்டது", ar: "تم التسليم" },
  "dash.emailOpened": { en: "Opened", ms: "Dibuka", zh: "已打开", ta: "திறக்கப்பட்டது", ar: "تم الفتح" },
  "dash.emailClicked": { en: "Clicked", ms: "Diklik", zh: "已点击", ta: "கிளிக் செய்யப்பட்டது", ar: "تم النقر" },
  "dash.emailBounced": { en: "Bounced", ms: "Bounce", zh: "已退回", ta: "திரும்பியது", ar: "ارتد" },
  "dash.emailComplaint": { en: "Complaint", ms: "Aduan", zh: "投诉", ta: "புகார்", ar: "شكوى" },
  "dash.emailNotSent": { en: "Not sent", ms: "Belum dihantar", zh: "未发送", ta: "அனுப்பப்படவில்லை", ar: "لم يُرسل" },
  "dash.emailStatusNote": { en: "Open tracking uses a tracking pixel in the receipt email. Only the open time is recorded, and no other personal data.", ms: "Penjejakan buka menggunakan piksel penjejakan dalam emel resit. Hanya masa dibuka direkod — tiada data peribadi lain.", zh: "打开追踪使用收据邮件中的追踪像素。仅记录打开时间——不记录其他个人数据。", ta: "திறப்பு கண்காணிப்பு ரசீது மின்னஞ்சலில் கண்காணிப்பு பிக்சலைப் பயன்படுத்துகிறது. திறந்த நேரம் மட்டுமே பதிவு செய்யப்படுகிறது — வேறு தனிப்பட்ட தரவு இல்லை.", ar: "يستخدم تتبع الفتح بكسل تتبع في بريد الإيصال. يُسجل وقت الفتح فقط — لا توجد بيانات شخصية أخرى." },
  // common
  "common.exportCsv": { en: "Export CSV", ms: "Eksport CSV", zh: "导出 CSV", ta: "CSV ஏற்றுமதி", ar: "تصدير CSV" },
  "common.workspace": { en: "Workspace", ms: "Workspace", zh: "工作区", ta: "பணியிடம்", ar: "مساحة العمل" },
  // The phone topbar's language <select> (the five-button toggle does not fit a
  // 360px bar once every button meets the 44x44 tap-target floor).
  "common.language": { en: "Language", ms: "Bahasa", zh: "语言", ta: "மொழி", ar: "اللغة" },
  // issue #144: create-workspace UI (always-visible switcher + inline form)
  "workspace.new": { en: "New workspace", ms: "Workspace baharu", zh: "新建工作区", ta: "புதிய பணியிடம்", ar: "مساحة عمل جديدة" },
  "workspace.name": { en: "Workspace name", ms: "Nama workspace", zh: "工作区名称", ta: "பணியிடப் பெயர்", ar: "اسم مساحة العمل" },
  "workspace.slug": { en: "Slug (a-z, 0-9, dash)", ms: "Slug (a-z, 0-9, dash)", zh: "Slug（a-z、0-9、连字符）", ta: "Slug (a-z, 0-9, dash)", ar: "Slug (a-z، 0-9، شرطة)" },
  "workspace.create": { en: "Create", ms: "Cipta", zh: "创建", ta: "உருவாக்கு", ar: "إنشاء" },
  "workspace.cancel": { en: "Cancel", ms: "Batal", zh: "取消", ta: "ரத்து", ar: "إلغاء" },
  "workspace.created": { en: "Workspace created ✓", ms: "Workspace dicipta ✓", zh: "工作区已创建 ✓", ta: "பணியிடம் உருவாக்கப்பட்டது ✓", ar: "تم إنشاء مساحة العمل ✓" },
  "workspace.createFail": { en: "Failed to create workspace.", ms: "Gagal cipta workspace.", zh: "创建工作区失败。", ta: "பணியிடம் உருவாக்க முடியவில்லை.", ar: "فشل إنشاء مساحة العمل." },
  "workspace.slugTaken": { en: "That slug is already taken.", ms: "Slug itu sudah digunakan.", zh: "该 Slug 已被占用。", ta: "அந்த slug ஏற்கனவே பயன்பாட்டில் உள்ளது.", ar: "هذا الـ slug مستخدم بالفعل." },
  "workspace.slugInvalid": { en: "Slug must be 3-40 chars, a-z, 0-9, dash.", ms: "Slug mesti 3-40 aksara, a-z, 0-9, dash.", zh: "Slug 必须为 3-40 个字符，仅限 a-z、0-9、连字符。", ta: "Slug 3-40 எழுத்துகள், a-z, 0-9, dash ஆக இருக்க வேண்டும்.", ar: "يجب أن يكون الـ slug من 3-40 حرفاً، a-z، 0-9، شرطة." },
  // Reserved subdomains (t_586ff0c3): the API refuses names JomForm owns or must
  // protect (infrastructure, docs/mcp/templates, support/billing/login-looking)
  // with a stable `reserved_subdomain` code. The merchant is told the name is
  // JomForm's — not that their typing was wrong — in their OWN language, because
  // the API's prose is English and is meant for curl/MCP callers (#694).
  "workspace.slugReserved": { en: "That name is reserved for JomForm and can't be used as your address. Please choose another.", ms: "Nama itu dikhaskan untuk JomForm dan tidak boleh digunakan sebagai alamat anda. Sila pilih yang lain.", zh: "该名称已保留给 JomForm，不能用作您的地址。请另选一个。", ta: "அந்தப் பெயர் JomForm-க்கு ஒதுக்கப்பட்டுள்ளது, உங்கள் முகவரியாகப் பயன்படுத்த முடியாது. வேறொன்றைத் தேர்ந்தெடுக்கவும்.", ar: "هذا الاسم محجوز لـ JomForm ولا يمكن استخدامه كعنوان لك. يرجى اختيار اسم آخر." },
  // The inline "?" doc link beside the slug field (AGENTS.md docs-parity rule 4).
  "workspace.whichNames": { en: "Which names are available?", ms: "Nama mana yang tersedia?", zh: "哪些名称可用？", ta: "எந்தப் பெயர்கள் கிடைக்கின்றன?", ar: "أي الأسماء متاحة؟" },
  // issue #164: multi-workspace login picker + default workspace
  "workspace.pickerTitle": { en: "Choose a workspace", ms: "Pilih workspace", zh: "选择工作区", ta: "பணியிடத்தைத் தேர்ந்தெடுக்கவும்", ar: "اختر مساحة عمل" },
  "workspace.pickerSubtitle": { en: "You have multiple workspaces. Pick which one to enter.", ms: "Anda ada beberapa workspace. Pilih yang mana untuk masuk.", zh: "您有多个工作区。请选择要进入的工作区。", ta: "உங்களுக்கு பல பணியிடங்கள் உள்ளன. எதை உள்ளிட வேண்டும் எனத் தேர்ந்தெடுக்கவும்.", ar: "لديك عدة مساحات عمل. اختر أيها تريد الدخول." },
  // The list state is reachable with exactly ONE workspace (a user who has not
  // set a default), and the sentence above is then a false claim: there is
  // nothing to pick. pickerCopyKeys selects this key from the ROW COUNT, so the
  // heading can never contradict the list under it.
  "workspace.pickerSingleSubtitle": { en: "You have one workspace. Enter it to continue.", ms: "Anda ada satu workspace. Masuk untuk meneruskan.", zh: "您只有一个工作区。请进入以继续。", ta: "உங்களுக்கு ஒரு பணியிடம் உள்ளது. தொடர அதில் நுழையுங்கள்.", ar: "لديك مساحة عمل واحدة. ادخلها للمتابعة." },
  // While the list is loading or unreadable the count is UNKNOWN, so neither
  // the single nor the multiple sentence is true; this one claims neither.
  "workspace.pickerNeutralSubtitle": { en: "Pick the workspace you want to enter.", ms: "Pilih workspace yang anda mahu masuk.", zh: "请选择要进入的工作区。", ta: "நீங்கள் நுழைய விரும்பும் பணியிடத்தைத் தேர்ந்தெடுக்கவும்.", ar: "اختر مساحة العمل التي تريد الدخول إليها." },
  "workspace.enter": { en: "Enter", ms: "Masuk", zh: "进入", ta: "உள்ளிடு", ar: "دخول" },
  "workspace.setDefault": { en: "Make this the default workspace", ms: "Jadikan workspace ini default", zh: "将此工作区设为默认", ta: "இதை இயல்புநிலை பணியிடமாக்கு", ar: "اجعل مساحة العمل هذه الافتراضية" },
  "workspace.defaultSet": { en: "Default workspace set ✓", ms: "Default workspace ditetapkan ✓", zh: "默认工作区已设置 ✓", ta: "இயல்புநிலை பணியிடம் அமைக்கப்பட்டது ✓", ar: "تم تعيين مساحة العمل الافتراضية ✓" },
  "workspace.clearDefault": { en: "Remove default workspace", ms: "Buang default workspace", zh: "移除默认工作区", ta: "இயல்புநிலை பணியிடத்தை நீக்கு", ar: "إزالة مساحة العمل الافتراضية" },
  "workspace.defaultCleared": { en: "Default workspace removed ✓", ms: "Default workspace dibuang ✓", zh: "默认工作区已移除 ✓", ta: "இயல்புநிலை பணியிடம் நீக்கப்பட்டது ✓", ar: "تمت إزالة مساحة العمل الافتراضية ✓" },
  "workspace.defaultLabel": { en: "Default", ms: "Lalai", zh: "默认", ta: "இயல்புநிலை", ar: "الافتراضية" },
  "workspace.setDefaultFail": { en: "Failed to set default workspace.", ms: "Gagal tetapkan default workspace.", zh: "设置默认工作区失败。", ta: "இயல்புநிலை பணியிடத்தை அமைக்க முடியவில்லை.", ar: "فشل تعيين مساحة العمل الافتراضية." },
  "workspace.clearDefaultFail": { en: "Failed to remove default workspace.", ms: "Gagal buang default workspace.", zh: "移除默认工作区失败。", ta: "இயல்புநிலை பணியிடத்தை நீக்க முடியவில்லை.", ar: "فشل إزالة مساحة العمل الافتراضية." },
  // Issue #503: the workspace picker's real states. "loading" is a genuine
  // loading flag (never `length === 0`), "loadFail" is the visible non-auth
  // error, "empty" is a genuinely empty list, and "common.retry" is the action
  // that keeps a failed load from being a dead end.
  "workspace.loading": { en: "Loading your workspaces…", ms: "Memuatkan workspace anda…", zh: "正在加载您的工作区…", ta: "உங்கள் பணியிடங்களை ஏற்றுகிறது…", ar: "جارٍ تحميل مساحات العمل…" },
  "workspace.loadFail": { en: "We couldn't load your workspaces. Please try again.", ms: "Kami tidak dapat memuatkan workspace anda. Sila cuba lagi.", zh: "无法加载您的工作区。请重试。", ta: "உங்கள் பணியிடங்களை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل مساحات العمل. يرجى المحاولة مرة أخرى." },
  "workspace.empty": { en: "You don't have any workspaces yet.", ms: "Anda belum mempunyai sebarang workspace.", zh: "您还没有任何工作区。", ta: "உங்களிடம் இன்னும் பணியிடங்கள் இல்லை.", ar: "ليس لديك أي مساحات عمل بعد." },
  // Ask-first onboarding: a user with NO workspace at all is asked to create one
  // before any create surface is reachable (new signups land here right after
  // login). These strings are the prompt itself — the one place that turns a
  // dead end into a working first run.
  "workspace.askPageTitle": { en: "Create your first workspace", ms: "Cipta workspace pertama anda", zh: "创建您的第一个工作区", ta: "உங்கள் முதல் பணியிடத்தை உருவாக்கவும்", ar: "أنشئ مساحة العمل الأولى" },
  "workspace.askPageSubtitle": { en: "Your workspace holds your forms, products and sales.", ms: "Workspace anda menyimpan borang, produk dan jualan anda.", zh: "您的工作区保存您的表单、产品和销售。", ta: "உங்கள் பணியிடம் உங்கள் படிவங்கள், தயாரிப்புகள் மற்றும் விற்பனைகளை வைத்திருக்கும்.", ar: "تحتوي مساحة عملك على نماذجك ومنتجاتك ومبيعاتك." },
  "workspace.askTitle": { en: "Create a workspace to get started", ms: "Cipta workspace untuk mula", zh: "创建一个工作区以开始", ta: "தொடங்க ஒரு பணியிடத்தை உருவாக்கவும்", ar: "أنشئ مساحة عمل للبدء" },
  "workspace.askBody": { en: "You need a workspace before you can create a form. It takes a moment: pick a name and a URL slug.", ms: "Anda perlukan workspace sebelum boleh cipta borang. Ambil masa sebentar — pilih nama dan slug URL.", zh: "创建表单前需要一个工作区。很快即可完成 — 选择名称和 URL slug。", ta: "படிவத்தை உருவாக்க ஒரு பணியிடம் தேவை. சிறிது நேரமே — ஒரு பெயரையும் URL slug-ஐயும் தேர்ந்தெடுக்கவும்.", ar: "تحتاج إلى مساحة عمل قبل إنشاء نموذج. الأمر سريع — اختر اسماً وslug للرابط." },
  "workspace.askHost": { en: "Your forms will live at {host}", ms: "Borang anda akan berada di {host}", zh: "您的表单将位于 {host}", ta: "உங்கள் படிவங்கள் {host} இல் இருக்கும்", ar: "ستكون نماذجك على {host}" },
  "workspace.docs": { en: "What is a workspace?", ms: "Apa itu workspace?", zh: "什么是工作区？", ta: "பணியிடம் என்பது என்ன?", ar: "ما هي مساحة العمل؟" },
  // The accessible status text for a page's in-flight load (see
  // components/PageLoading.tsx). Rendered sr-only inside the shell's skeleton:
  // the skeleton bars themselves are decorative, so a screen reader would
  // otherwise get no signal at all that the page is still loading.
  "common.loading": { en: "Loading…", ms: "Memuatkan…", zh: "正在加载…", ta: "ஏற்றுகிறது…", ar: "جارٍ التحميل…" },
  "common.retry": { en: "Retry", ms: "Cuba semula", zh: "重试", ta: "மீண்டும் முயற்சி", ar: "إعادة المحاولة" },
  // Card t_c83ecf3e: the label on the phone layout's per-row disclosure — it
  // holds the table columns that the desktop layout shows as their own columns.
  "common.details": { en: "Details", ms: "Butiran", zh: "详情", ta: "விவரங்கள்", ar: "التفاصيل" },
  // Card t_48319be9: the session bootstrap read did not produce an answer (a
  // fault — a 5xx or a dropped connection), which is NOT the same as being signed
  // out, so the app says what happened and offers a retry instead of silently
  // bouncing the merchant to the landing page.
  "common.sessionUnavailable": { en: "We couldn't start your session. Please try again.", ms: "Kami tidak dapat memulakan sesi anda. Sila cuba lagi.", zh: "无法启动您的会话。请重试。", ta: "உங்கள் அமர்வைத் தொடங்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر بدء جلستك. يرجى المحاولة مرة أخرى." },
  // Issue #503 (audit): the same visible-failure text for the other list pages
  // that used to swallow a non-ok response into an empty list.
  "forms.loadFail": { en: "We couldn't load your forms. Please try again.", ms: "Kami tidak dapat memuatkan borang anda. Sila cuba lagi.", zh: "无法加载您的表单。请重试。", ta: "உங்கள் படிவங்களை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل نماذجك. يرجى المحاولة مرة أخرى." },
  "prod.loadFailList": { en: "We couldn't load your products. Please try again.", ms: "Kami tidak dapat memuatkan produk anda. Sila cuba lagi.", zh: "无法加载您的商品。请重试。", ta: "உங்கள் தயாரிப்புகளை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل منتجاتك. يرجى المحاولة مرة أخرى." },
  "sub.loadFail": { en: "We couldn't load your subscriptions. Please try again.", ms: "Kami tidak dapat memuatkan langganan anda. Sila cuba lagi.", zh: "无法加载您的订阅。请重试。", ta: "உங்கள் சந்தாக்களை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل اشتراكاتك. يرجى المحاولة مرة أخرى." },
  "tplmg.loadFail": { en: "We couldn't load your templates. Please try again.", ms: "Kami tidak dapat memuatkan templat anda. Sila cuba lagi.", zh: "无法加载您的模板。请重试。", ta: "உங்கள் வார்ப்புருக்களை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل قوالبك. يرجى المحاولة مرة أخرى." },
  "media.loadFail": { en: "We couldn't load your media. Please try again.", ms: "Kami tidak dapat memuatkan media anda. Sila cuba lagi.", zh: "无法加载您的媒体。请重试。", ta: "உங்கள் ஊடகத்தை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل وسائطك. يرجى المحاولة مرة أخرى." },
  "bld.prodLoadFail": { en: "We couldn't load your products. Please try again.", ms: "Kami tidak dapat memuatkan produk anda. Sila cuba lagi.", zh: "无法加载您的商品。请重试。", ta: "உங்கள் தயாரிப்புகளை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل منتجاتك. يرجى المحاولة مرة أخرى." },
  "bld.formLoadFail": { en: "We couldn't load this form. Please try again.", ms: "Kami tidak dapat memuatkan borang ini. Sila cuba lagi.", zh: "无法加载此表单。请重试。", ta: "இந்தப் படிவத்தை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل هذا النموذج. يرجى المحاولة مرة أخرى." },
  "bld.saveFail": { en: "Failed to save.", ms: "Gagal simpan.", zh: "保存失败。", ta: "சேமிக்க முடியவில்லை.", ar: "فشل الحفظ." },
  "bld.statusSaveFail": { en: "Failed to set the form status.", ms: "Gagal tetapkan status borang.", zh: "设置表单状态失败。", ta: "படிவ நிலையை அமைக்க முடியவில்லை.", ar: "فشل تعيين حالة النموذج." },
  // issue #163: workspace members management UI + role-gated actions
  "set.tab.members": { en: "Members", ms: "Ahli", zh: "成员", ta: "உறுப்பினர்கள்", ar: "الأعضاء" },
  "mem.title": { en: "Workspace members", ms: "Ahli ruang kerja", zh: "工作区成员", ta: "பணியிட உறுப்பினர்கள்", ar: "أعضاء مساحة العمل" },
  "mem.loadFail": { en: "We couldn't load the member list. Please try again.", ms: "Kami tidak dapat memuatkan senarai ahli. Sila cuba lagi.", zh: "无法加载成员列表。请重试。", ta: "உறுப்பினர் பட்டியலை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل قائمة الأعضاء. يرجى المحاولة مرة أخرى." },
  "mem.inviteTitle": { en: "Invite a member", ms: "Jemput ahli", zh: "邀请成员", ta: "உறுப்பினரை அழை", ar: "دعوة عضو" },
  "mem.inviteEmail": { en: "Email address", ms: "Alamat emel", zh: "邮箱地址", ta: "மின்னஞ்சல் முகவரி", ar: "البريد الإلكتروني" },
  "mem.inviteRole": { en: "Role", ms: "Peranan", zh: "角色", ta: "பங்கு", ar: "الدور" },
  "mem.invite": { en: "Send invite", ms: "Hantar jemputan", zh: "发送邀请", ta: "அழைப்பு அனுப்பு", ar: "إرسال الدعوة" },
  "mem.invited": { en: "Invite sent ✓", ms: "Jemputan dihantar ✓", zh: "邀请已发送 ✓", ta: "அழைப்பு அனுப்பப்பட்டது ✓", ar: "تم إرسال الدعوة ✓" },
  "mem.added": { en: "Member added ✓", ms: "Ahli ditambah ✓", zh: "成员已添加 ✓", ta: "உறுப்பினர் சேர்க்கப்பட்டார் ✓", ar: "تمت إضافة العضو ✓" },
  "mem.inviteFail": { en: "Failed to send invite: {error}", ms: "Gagal hantar jemputan: {error}", zh: "发送邀请失败：{error}", ta: "அழைப்பு அனுப்பத் தோல்வி: {error}", ar: "فشل إرسال الدعوة: {error}" },
  "mem.membersList": { en: "Members", ms: "Ahli", zh: "成员", ta: "உறுப்பினர்கள்", ar: "الأعضاء" },
  "mem.noMembers": { en: "No members yet.", ms: "Belum ada ahli.", zh: "暂无成员。", ta: "இன்னும் உறுப்பினர்கள் இல்லை.", ar: "لا أعضاء بعد." },
  "mem.pendingTitle": { en: "Pending invites", ms: "Jemputan menunggu", zh: "待处理邀请", ta: "நிலுவை அழைப்புகள்", ar: "دعوات معلقة" },
  "mem.noPending": { en: "No pending invites.", ms: "Tiada jemputan menunggu.", zh: "暂无待处理邀请。", ta: "நிலுவை அழைப்புகள் இல்லை.", ar: "لا دعوات معلقة." },
  "mem.cancelInvite": { en: "Cancel", ms: "Batal", zh: "取消", ta: "ரத்து", ar: "إلغاء" },
  "mem.inviteCancelled": { en: "Invite cancelled ✓", ms: "Jemputan dibatalkan ✓", zh: "邀请已取消 ✓", ta: "அழைப்பு ரத்து செய்யப்பட்டது ✓", ar: "تم إلغاء الدعوة ✓" },
  "mem.changeRole": { en: "Change role", ms: "Tukar peranan", zh: "更改角色", ta: "பங்கு மாற்று", ar: "تغيير الدور" },
  "mem.roleUpdated": { en: "Role updated ✓", ms: "Peranan dikemas kini ✓", zh: "角色已更新 ✓", ta: "பங்கு புதுப்பிக்கப்பட்டது ✓", ar: "تم تحديث الدور ✓" },
  "mem.roleUpdateFail": { en: "Failed to update role: {error}", ms: "Gagal kemas kini peranan: {error}", zh: "更新角色失败：{error}", ta: "பங்கு புதுப்பிக்கத் தோல்வி: {error}", ar: "فشل تحديث الدور: {error}" },
  "mem.remove": { en: "Remove", ms: "Buang", zh: "移除", ta: "நீக்கு", ar: "إزالة" },
  "mem.removeConfirm": { en: "Remove {name} from this workspace? They will lose access immediately.", ms: "Buang {name} dari ruang kerja ini? Mereka akan hilang akses serta-merta.", zh: "将 {name} 移出此工作区？他们将立即失去访问权限。", ta: "{name} ஐ இந்த பணியிடத்திலிருந்து நீக்கவா? அவர்கள் உடனடியாக அணுகலை இழப்பார்கள்.", ar: "إزالة {name} من مساحة العمل هذه؟ سيفقدون الوصول فوراً." },
  "mem.removed": { en: "Member removed ✓", ms: "Ahli dibuang ✓", zh: "成员已移除 ✓", ta: "உறுப்பினர் நீக்கப்பட்டார் ✓", ar: "تمت إزالة العضو ✓" },
  "mem.removeFail": { en: "Failed to remove member: {error}", ms: "Gagal buang ahli: {error}", zh: "移除成员失败：{error}", ta: "உறுப்பினரை நீக்கத் தோல்வி: {error}", ar: "فشل إزالة العضو: {error}" },
  "mem.owner": { en: "Owner", ms: "Pemilik", zh: "所有者", ta: "உரிமையாளர்", ar: "المالك" },
  "mem.you": { en: "you", ms: "anda", zh: "您", ta: "நீங்கள்", ar: "أنت" },
  "mem.adminOnly": { en: "Only workspace admins can manage members.", ms: "Hanya admin ruang kerja boleh urus ahli.", zh: "只有工作区管理员可以管理成员。", ta: "பணியிட நிர்வாகிகள் மட்டுமே உறுப்பினர்களை நிர்வகிக்க முடியும்.", ar: "يمكن لمسؤولي مساحة العمل فقط إدارة الأعضاء." },
  "mem.readOnlyNote": { en: "You have read-only access to this workspace.", ms: "Anda mempunyai akses baca sahaja ke ruang kerja ini.", zh: "您对此工作区拥有只读访问权限。", ta: "இந்த பணியிடத்திற்கு உங்களுக்கு படிக்க-மட்டும் அணுகல் உள்ளது.", ar: "لديك وصول للقراءة فقط إلى مساحة العمل هذه." },
  "role.read_only": { en: "Read only", ms: "Baca sahaja", zh: "只读", ta: "படிக்க மட்டும்", ar: "قراءة فقط" },
  "role.manage": { en: "Manage", ms: "Urus", zh: "管理", ta: "நிர்வகி", ar: "إدارة" },
  "role.admin": { en: "Admin", ms: "Admin", zh: "管理员", ta: "நிர்வாகி", ar: "مدير" },
  "role.owner": { en: "Owner", ms: "Pemilik", zh: "所有者", ta: "உரிமையாளர்", ar: "المالك" },
  "role.hint.read_only": { en: "Can view forms, products and sales. Cannot edit.", ms: "Boleh lihat borang, produk & jualan — tiada edit.", zh: "可查看表单、商品和销售 — 不可编辑。", ta: "படிவங்கள், தயாரிப்புகள், விற்பனைகளைப் பார்க்கலாம் — திருத்த முடியாது.", ar: "يمكنه عرض النماذج والمنتجات والمبيعات — بدون تعديل." },
  "role.hint.manage": { en: "Can edit forms, products and sales. Cannot change members or payment settings.", ms: "Boleh edit borang, produk & jualan — tiada tetapan ahli atau pembayaran.", zh: "可编辑表单、商品和销售 — 不可管理成员或支付设置。", ta: "படிவங்கள், தயாரிப்புகள், விற்பனைகளைத் திருத்தலாம் — உறுப்பினர் அல்லது கட்டண அமைப்புகள் இல்லை.", ar: "يمكنه تعديل النماذج والمنتجات والمبيعات — بدون إعدادات الأعضاء أو الدفع." },
  "role.hint.admin": { en: "Full access, including members and payment settings.", ms: "Akses penuh, termasuk ahli & tetapan pembayaran.", zh: "完全访问，包括成员和支付设置。", ta: "முழு அணுகல், உறுப்பினர்கள் மற்றும் கட்டண அமைப்புகள் உட்பட.", ar: "وصول كامل، بما في ذلك الأعضاء وإعدادات الدفع." },
  "common.confirm": { en: "Confirm", ms: "Sahkan", zh: "确认", ta: "உறுதிப்படுத்து", ar: "تأكيد" },
  "common.prev": { en: "Prev", ms: "Sebelum", zh: "上一页", ta: "முந்தையது", ar: "السابق" },
  "common.next": { en: "Next", ms: "Seterusnya", zh: "下一页", ta: "அடுத்து", ar: "التالي" },
  // field default labels (builder)
  "blk.fullName": { en: "Full name", ms: "Nama penuh", zh: "全名", ta: "முழு பெயர்", ar: "الاسم الكامل" },
  "blk.phoneNum": { en: "Phone number", ms: "Nombor telefon", zh: "电话号码", ta: "தொலைபேசி எண்", ar: "رقم الهاتف" },
  "blk.addNote": { en: "Additional note", ms: "Nota tambahan", zh: "补充说明", ta: "கூடுதல் குறிப்பு", ar: "ملاحظة إضافية" },
  // products page
  "prod.title": { en: "Products", ms: "Produk", zh: "商品", ta: "தயாரிப்புகள்", ar: "المنتجات" },
  "prod.subtitle": { en: "One-time items and subscriptions you sell", ms: "Produk one-time & langganan yang anda jual", zh: "管理您销售的商品", ta: "விற்கும் தயாரிப்புகளை நிர்வகிக்கவும்", ar: "إدارة المنتجات التي تبيعها" },
  "prod.add": { en: "Add product", ms: "Tambah produk", zh: "+ 添加商品", ta: "+ சேர்க்க", ar: "+ إضافة منتج" },
  "prod.name": { en: "Product name", ms: "Nama produk", zh: "名称", ta: "பெயர்", ar: "الاسم" },
  "prod.price": { en: "Price ({cur})", ms: "Harga ({cur})", zh: "价格（{cur}）", ta: "விலை ({cur})", ar: "السعر ({cur})" },
  "prod.type": { en: "Type", ms: "Jenis", zh: "类型", ta: "வகை", ar: "النوع" },
  "prod.oneTime": { en: "One-time", ms: "One-time", zh: "一次性", ta: "ஒரு முறை", ar: "لمرة واحدة" },
  "prod.subscription": { en: "Subscription", ms: "Langganan", zh: "订阅", ta: "சந்தா", ar: "اشتراك" },
  "prod.monthly": { en: "Monthly", ms: "Bulanan", zh: "每月", ta: "மாதந்தோறும்", ar: "شهرياً" },
  "prod.yearly": { en: "Yearly", ms: "Tahunan", zh: "每年", ta: "ஆண்டுதோறும்", ar: "سنوياً" },
  "prod.empty": { en: "No products yet. Use “New product” above to add your first.", ms: "Belum ada produk — guna “Produk baharu” di atas untuk tambah yang pertama.", zh: "还没有商品。用上方的“新建商品”添加第一个。", ta: "இன்னும் தயாரிப்புகள் இல்லை. மேலே உள்ள “புதிய தயாரிப்பு” மூலம் முதல் ஒன்றைச் சேர்க்கவும்.", ar: "لا توجد منتجات بعد. استخدم “منتج جديد” في الأعلى لإضافة أول منتج." },
  // Card t_bb0d0bee: the quick-create panel's route to the full editor. The
  // panel is the ONE creation entry point; /products/new is its secondary path
  // (variations, images, payment link) and must stay reachable (card t_7eb3b623).
  "prod.fullEditor": { en: "More options: variations, images", ms: "Pilihan lanjutan: variasi, imej", zh: "更多选项：变体、图片", ta: "மேலும் விருப்பங்கள்: மாறுபாடுகள், படங்கள்", ar: "خيارات إضافية: الأشكال والصور" },
  "prod.stock": { en: "Stock (blank = ∞)", ms: "Stok (kosong = ∞)", zh: "库存（留空 = ∞）", ta: "சரக்கு (காலியாக = ∞)", ar: "المخزون (فارغ = ∞)" },
  "prod.inStock": { en: "{n} in stock", ms: "{n} dalam stok", zh: "库存 {n}", ta: "சரக்கில் {n}", ar: "{n} في المخزون" },
  "prod.soldOut": { en: "SOLD OUT", ms: "HABIS", zh: "售罄", ta: "விற்றுத் தீர்ந்தது", ar: "نفدت الكمية" },
  "prod.failedAdd": { en: "Failed to add product", ms: "Gagal tambah produk", zh: "添加商品失败", ta: "பொருள் சேர்க்க தோல்வி", ar: "فشل إضافة المنتج" },
  // issue #122: WooCommerce-style products list (search/sort/filter/pagination/bulk/edit/status)
  "prod.active": { en: "Active", ms: "Aktif", zh: "在售", ta: "செயலில்", ar: "نشط" },
  "prod.archived": { en: "Archived", ms: "Diarkib", zh: "已归档", ta: "தொகுக்கப்பட்டது", ar: "مؤرشف" },
  "prod.countItems": { en: "{n} items", ms: "{n} item", zh: "{n} 项", ta: "{n} உருப்படிகள்", ar: "{n} عنصر" },
  "prod.searchPlaceholder": { en: "Search products…", ms: "Cari produk…", zh: "搜索商品…", ta: "தயாரிப்புகளைத் தேடு…", ar: "ابحث عن المنتجات…" },
  "prod.filterType": { en: "Type filter", ms: "Tapis jenis", zh: "类型筛选", ta: "வகை வடிப்பான்", ar: "تصفية النوع" },
  "prod.filterAll": { en: "All types", ms: "Semua jenis", zh: "所有类型", ta: "அனைத்து வகைகள்", ar: "كل الأنواع" },
  "prod.filterStock": { en: "Stock filter", ms: "Tapis stok", zh: "库存筛选", ta: "சரக்கு வடிப்பான்", ar: "تصفية المخزون" },
  "prod.filterStockAll": { en: "All stock", ms: "Semua stok", zh: "所有库存", ta: "அனைத்து சரக்கு", ar: "كل المخزون" },
  "prod.inStockShort": { en: "In stock", ms: "Dalam stok", zh: "有库存", ta: "சரக்கில் உள்ளது", ar: "متوفر" },
  "prod.outOfStock": { en: "Out of stock", ms: "Kehabisan stok", zh: "缺货", ta: "சரக்கு இல்லை", ar: "نفد المخزون" },
  "prod.stockShort": { en: "Stock", ms: "Stok", zh: "库存", ta: "சரக்கு", ar: "المخزون" },
  "prod.date": { en: "Date", ms: "Tarikh", zh: "日期", ta: "தேதி", ar: "التاريخ" },
  "prod.pageOf": { en: "page {page} of {total}", ms: "halaman {page} daripada {total}", zh: "第 {page} 页，共 {total} 页", ta: "பக்கம் {page} / {total}", ar: "صفحة {page} من {total}" },
  "prod.bulkArchive": { en: "Archive selected", ms: "Arkib yang dipilih", zh: "归档所选", ta: "தேர்ந்தெடுத்ததை தொகு", ar: "أرشفة المحدد" },
  "prod.bulkArchiveConfirm": { en: "Archive {n} product(s)? They will be soft-archived (hidden from buyers) and can be reactivated later.", ms: "Arkib {n} produk? Ia akan diarkib lembut (sorok dari pembeli) dan boleh diaktifkan semula.", zh: "归档 {n} 件商品？它们将被软归档（对买家隐藏），之后可重新激活。", ta: "{n} தயாரிப்புகளை தொகுக்கவா? அவை மென்-தொகுக்கப்படும் (வாங்குபவர்களுக்கு மறைக்கப்படும்), பின்னர் மீண்டும் செயல்படுத்தலாம்.", ar: "أرشفة {n} منتج؟ ستنشأ أرشفة ناعمة (مخفية عن المشترين) ويمكن إعادة تنشيطها لاحقاً." },
  "prod.archiveOneConfirm": { en: "Move “{name}” to the trash? It leaves the catalogue and can be restored from Trash.", ms: "Pindahkan “{name}” ke tong sampah? Ia keluar dari katalog dan boleh dipulihkan dari Tong Sampah.", zh: "将“{name}”移入回收站？它将离开商品目录，可从回收站恢复。", ta: "\"{name}\" ஐ குப்பைத் தொட்டிக்கு நகர்த்தவா? அது பட்டியலிலிருந்து விலகும், குப்பைத் தொட்டியிலிருந்து மீட்டமைக்கலாம்.", ar: "نقل «{name}» إلى سلة المهملات؟ سيخرج من الكتالوج ويمكن استعادته من سلة المهملات." },
  "prod.archiveOneTitle": { en: "Move to trash", ms: "Pindah ke tong sampah", zh: "移入回收站", ta: "குப்பைத் தொட்டிக்கு நகர்த்து", ar: "نقل إلى سلة المهملات" },
  "prod.toggleStatusTitle": { en: "Toggle active/archived", ms: "Tukar aktif/arkib", zh: "切换在售/归档", ta: "செயலில்/தொகுப்பு மாற்று", ar: "تبديل نشط/مؤرشف" },
  "prod.editTitle": { en: "Edit product", ms: "Edit produk", zh: "编辑商品", ta: "தயாரிப்பைத் திருத்து", ar: "تحرير المنتج" },
  "prod.billingPeriod": { en: "Billing period", ms: "Tempoh bil", zh: "计费周期", ta: "பில் காலம்", ar: "فترة الفوترة" },
  "prod.failedUpdate": { en: "Failed to update product", ms: "Gagal kemas kini produk", zh: "更新商品失败", ta: "தயாரிப்பு புதுப்பிக்க தோல்வி", ar: "فشل تحديث المنتج" },
  // issue #124: full product editor (WC-style)
  "prod.description": { en: "Description", ms: "Penerangan", zh: "描述", ta: "விளக்கம்", ar: "الوصف" },
  "prod.descriptionPh": { en: "Describe this product to buyers…", ms: "Terangkan produk ini kepada pembeli…", zh: "向买家描述此商品…", ta: "இந்த தயாரிப்பை வாங்குபவர்களுக்கு விவரிக்கவும்…", ar: "صف هذا المنتج للمشترين…" },
  "prod.general": { en: "General", ms: "Am", zh: "常规", ta: "பொது", ar: "عام" },
  "prod.subscriptionTab": { en: "Subscription", ms: "Langganan", zh: "订阅", ta: "சந்தா", ar: "اشتراك" },
  "prod.inventory": { en: "Inventory", ms: "Inventori", zh: "库存", ta: "சரக்கு", ar: "المخزون" },
  "prod.media": { en: "Media", ms: "Media", zh: "媒体", ta: "ஊடகம்", ar: "الوسائط" },
  "prod.images": { en: "Product images", ms: "Gambar produk", zh: "商品图片", ta: "தயாரிப்பு படங்கள்", ar: "صور المنتج" },
  "prod.uploadImage": { en: "Upload image", ms: "Muat naik gambar", zh: "上传图片", ta: "படத்தைப் பதிவேற்று", ar: "رفع صورة" },
  "prod.uploading": { en: "Uploading…", ms: "Memuat naik…", zh: "上传中…", ta: "பதிவேற்றுகிறது…", ar: "جارٍ الرفع…" },
  "prod.uploadFail": { en: "Image upload failed", ms: "Gagal muat naik gambar", zh: "图片上传失败", ta: "பட பதிவேற்றம் தோல்வி", ar: "فشل رفع الصورة" },
  "prod.addImage": { en: "Add image", ms: "Tambah gambar", zh: "添加图片", ta: "படம் சேர்", ar: "إضافة صورة" },
  "prod.removeImage": { en: "Remove", ms: "Buang", zh: "移除", ta: "அகற்று", ar: "إزالة" },
  "prod.moveUp": { en: "Move up", ms: "Naik ke atas", zh: "上移", ta: "மேலே நகர்த்து", ar: "تحريك لأعلى" },
  "prod.moveDown": { en: "Move down", ms: "Turun ke bawah", zh: "下移", ta: "கீழே நகர்த்து", ar: "تحريك لأسفل" },
  "prod.imagesNote": { en: "First image is the cover thumbnail shown to buyers.", ms: "Gambar pertama ialah gambar muka yang dipaparkan kepada pembeli.", zh: "第一张图片是展示给买家的封面缩略图。", ta: "முதல் படம் வாங்குபவர்களுக்குக் காட்டப்படும் அட்டை சிறுபடம்.", ar: "الصورة الأولى هي الصورة المصغرة للغلاف المعروضة للمشترين." },
  "prod.trial": { en: "Trial days", ms: "Hari percubaan", zh: "试用天数", ta: "சோதனை நாட்கள்", ar: "أيام التجربة" },
  "prod.trialHint": { en: "0 = no free trial", ms: "0 = tiada percubaan percuma", zh: "0 = 无免费试用", ta: "0 = இலவச சோதனை இல்லை", ar: "0 = لا توجد نسخة تجريبية مجانية" },
  // issue #152: subscription billing count (infinite vs fixed N charges)
  "prod.billingCount": { en: "Billing count", ms: "Bilangan bil", zh: "计费次数", ta: "பில் எண்ணிக்கை", ar: "عدد الفوترة" },
  "prod.chargesInfinite": { en: "Infinite (recurring)", ms: "Tak terhad (berulang)", zh: "无限（循环）", ta: "வரம்பற்ற (தொடர்ச்சி)", ar: "غير محدود (متكرر)" },
  "prod.chargesFixed": { en: "Fixed N times", ms: "Tetap N kali", zh: "固定 N 次", ta: "நிலையான N முறை", ar: "N مرة ثابتة" },
  "prod.chargesN": { en: "N charges", ms: "N caj", zh: "N 次扣费", ta: "N கட்டணங்கள்", ar: "N شحنة" },
  // round 13c: next-charge date policy after a successful charge.
  "prod.scheduleMode": { en: "Next charge date", ms: "Tarikh caj seterusnya", zh: "下次扣费日期", ta: "அடுத்த கட்டண தேதி", ar: "تاريخ الدفع التالي" },
  "prod.schedFromPayment": { en: "From the day it succeeds (drifts if late)", ms: "Dari hari ia berjaya (beralih jika lewat)", zh: "从成功当天起算（迟付会顺延）", ta: "வெற்றியடைந்த நாளிலிருந்து (தாமதமானால் நகரும்)", ar: "من يوم النجاح (يتغير عند التأخر)" },
  "prod.schedCatchUp": { en: "Keep original dates, collect missed months", ms: "Kekal tarikh asal — bulan tertinggal tetap dicaj", zh: "保持原定日期 — 补收错过的月份", ta: "அசல் தேதிகளை வைத்திரு — தவறிய மாதங்களை வசூலி", ar: "الاحتفاظ بالتواريخ الأصلية — تحصيل الأشهر الفائتة" },
  "prod.schedSkipMissed": { en: "Keep original dates, skip missed months", ms: "Kekal tarikh asal — bulan tertinggal dilangkau", zh: "保持原定日期 — 跳过错过的月份", ta: "அசல் தேதிகளை வைத்திரு — தவறிய மாதங்களை தவிர்", ar: "الاحتفاظ بالتواريخ الأصلية — تخطي الأشهر الفائتة" },
  "prod.scheduleModeHint": { en: "If a charge is late: keep the original billing dates, or start counting from the day it finally succeeds.", ms: "Jika caj lewat: kekalkan tarikh asal, atau mula kira dari hari ia berjaya.", zh: "若扣费延迟：保持原定日期，或从成功当天重新起算。", ta: "கட்டணம் தாமதமானால்: அசல் தேதிகளை வைத்திரு, அல்லது வெற்றியடைந்த நாளிலிருந்து கணக்கிடு.", ar: "إذا تأخر الدفع: احتفظ بالتواريخ الأصلية أو ابدأ العد من يوم النجاح." },
  "prod.subscriptionDocs": { en: "Subscriptions & recurring billing documentation", ms: "Dokumentasi langganan & bil berulang", zh: "订阅与定期账单文档", ta: "சந்தா மற்றும் தொடர் பில்லிங் ஆவணம்", ar: "وثائق الاشتراك والفوترة المتكررة" },
  "prod.chargesNLabel": { en: "{n} charges", ms: "{n} caj", zh: "{n} 次扣费", ta: "{n} கட்டணங்கள்", ar: "{n} شحنة" },
  "prod.chargesHint": { en: "Infinite bills forever; N stops after the Nth charge.", ms: "Tak terhad bil selamanya; N berhenti selepas caj ke-N.", zh: "无限则永久计费；N 表示第 N 次扣费后停止。", ta: "வரம்பற்றது என்றும் பில் செய்யும்; N என்பது N-வது கட்டணத்திற்குப் பிறகு நிறுத்தும்.", ar: "غير المحدود يفوتر للأبد؛ N يتوقف بعد الشحنة رقم N." },
  "prod.noStockNote": { en: "Leave stock blank for unlimited.", ms: "Kosongkan stok untuk unlimited.", zh: "库存留空表示不限量。", ta: "வரம்பற்றதற்கு சரக்கை காலியாக விடவும்.", ar: "اترك المخزون فارغاً لغير المحدود." },
  "prod.status": { en: "Status / visibility", ms: "Status / keterlihatan", zh: "状态 / 可见性", ta: "நிலை / தெரிவுநிலை", ar: "الحالة / الظهور" },
  "prod.activeHint": { en: "Inactive products are hidden on the buyer form and show as Archived in the list. Buyers won't be able to purchase them until reactivated.", ms: "Produk tidak aktif disorok pada borang pembeli dan dipaparkan sebagai Diarkib dalam senarai. Pembeli tidak boleh membeli sehingga diaktifkan semula.", zh: "不活跃的商品在买家表单中隐藏，并在列表中显示为已归档。重新激活前买家无法购买。", ta: "செயலில் இல்லாத தயாரிப்புகள் வாங்குபவர் படிவத்தில் மறைக்கப்படும், பட்டியலில் தொகுக்கப்பட்டவை எனக் காட்டப்படும். மீண்டும் இயக்கப்படும் வரை வாங்க முடியாது.", ar: "تُخفى المنتجات غير النشطة في نموذج المشتري وتظهر كمؤرشفة في القائمة. لن يتمكن المشترون من شرائها حتى إعادة تنشيطها." },
  "prod.cancel": { en: "Cancel", ms: "Batal", zh: "取消", ta: "ரத்து", ar: "إلغاء" },
  "prod.unsaved": { en: "You have unsaved changes. Discard them?", ms: "Anda ada perubahan belum disimpan. Buang?", zh: "您有未保存的更改。要放弃吗？", ta: "சேமிக்கப்படாத மாற்றங்கள் உள்ளன. அவற்றை நிராகரிக்கவா?", ar: "لديك تغييرات غير محفوظة. هل تريد تجاهلها؟" },
  "prod.updated": { en: "Product saved", ms: "Produk disimpan", zh: "商品已保存", ta: "தயாரிப்பு சேமிக்கப்பட்டது", ar: "تم حفظ المنتج" },
  "prod.inactiveNote": { en: "Archived (hidden from buyers)", ms: "Diarkib (disorok dari pembeli)", zh: "已归档（对买家隐藏）", ta: "தொகுக்கப்பட்டது (வாங்குபவர்களுக்கு மறைக்கப்பட்டது)", ar: "مؤرشف (مخفي عن المشترين)" },
  // issue #151: dedicated product editor page + variations
  "prod.newTitle": { en: "New product", ms: "Produk baharu", zh: "新建商品", ta: "புதிய தயாரிப்பு", ar: "منتج جديد" },
  // Card t_7eb3b623: the row's link to the full-page editor — the inline dialog
  // is the quick edit, this is where variations and images live.
  "prod.openEditor": { en: "Open editor", ms: "Buka editor", zh: "打开编辑器", ta: "எடிட்டரைத் திற", ar: "فتح المحرر" },
  "prod.backToProducts": { en: "← Back to products", ms: "← Balik ke produk", zh: "← 返回商品", ta: "← தயாரிப்புகளுக்குத் திரும்பு", ar: "← العودة إلى المنتجات" },
  "prod.productType": { en: "Product type", ms: "Jenis produk", zh: "商品类型", ta: "தயாரிப்பு வகை", ar: "نوع المنتج" },
  "prod.simple": { en: "Simple", ms: "Simple", zh: "简单", ta: "எளிய", ar: "بسيط" },
  "prod.variable": { en: "Variable", ms: "Variable", zh: "多规格", ta: "மாறுபடும்", ar: "متغير" },
  "prod.simpleHint": { en: "One price, one stock, product-level images.", ms: "Satu harga, satu stok, gambar peringkat produk.", zh: "单一价格、单一库存、商品级图片。", ta: "ஒரு விலை, ஒரு சரக்கு, தயாரிப்பு நிலை படங்கள்.", ar: "سعر واحد ومخزون واحد وصور على مستوى المنتج." },
  "prod.variableHint": { en: "Multiple options (e.g. sizes, colours), each with its own price, stock and image.", ms: "Pelbagai pilihan (cth: saiz, warna), setiap satu dengan harga, stok dan gambar sendiri.", zh: "多个选项（如尺寸、颜色），每个都有自己的价格、库存和图片。", ta: "பல விருப்பங்கள் (எ.கா. அளவுகள், நிறங்கள்), ஒவ்வொன்றும் அதன் சொந்த விலை, சரக்கு மற்றும் படத்துடன்.", ar: "خيارات متعددة (مثل المقاسات والألوان)، لكل منها سعره ومخزونه وصورته." },
  "prod.variations": { en: "Variations", ms: "Variasi", zh: "规格", ta: "மாறுபாடுகள்", ar: "الخيارات" },
  "prod.addVariation": { en: "Add variation", ms: "Tambah variasi", zh: "添加规格", ta: "மாறுபாடு சேர்", ar: "إضافة خيار" },
  "prod.variationName": { en: "Variation name", ms: "Nama variasi", zh: "规格名称", ta: "மாறுபாடு பெயர்", ar: "اسم الخيار" },
  "prod.variationNamePh": { en: "e.g. Size M, Red", ms: "cth: Saiz M, Merah", zh: "例如：M 码、红色", ta: "எ.கா: அளவு M, சிவப்பு", ar: "مثال: مقاس M، أحمر" },
  "prod.variationPrice": { en: "Price ({cur})", ms: "Harga ({cur})", zh: "价格（{cur}）", ta: "விலை ({cur})", ar: "السعر ({cur})" },
  "prod.variationStock": { en: "Stock (blank = ∞)", ms: "Stok (kosong = ∞)", zh: "库存（留空 = ∞）", ta: "சரக்கு (காலியாக = ∞)", ar: "المخزون (فارغ = ∞)" },
  "prod.variationImage": { en: "Image", ms: "Gambar", zh: "图片", ta: "படம்", ar: "صورة" },
  "prod.removeVariation": { en: "Remove", ms: "Buang", zh: "移除", ta: "அகற்று", ar: "إزالة" },
  "prod.variationRequired": { en: "Each variation needs a name and price.", ms: "Setiap variasi perlu nama dan harga.", zh: "每个规格都需要名称和价格。", ta: "ஒவ்வொரு மாறுபாட்டிற்கும் பெயர் மற்றும் விலை தேவை.", ar: "كل خيار يحتاج إلى اسم وسعر." },
  "prod.variationCount": { en: "{n} variation(s)", ms: "{n} variasi", zh: "{n} 个规格", ta: "{n} மாறுபாடு(கள்)", ar: "{n} خيار" },
  "prod.simplePrice": { en: "Price ({cur})", ms: "Harga ({cur})", zh: "价格（{cur}）", ta: "விலை ({cur})", ar: "السعر ({cur})" },
  // Card t_06851d88: pay-what-you-want (donation) products. The buyer types the
  // amount on the form; these limits are what the SERVER checks it against.
  "prod.customAmount": { en: "Pay what you want (donation)", ms: "Bayar ikut suka (derma)", zh: "随意支付（捐款）", ta: "விருப்பத் தொகை (நன்கொடை)", ar: "ادفع ما تشاء (تبرع)" },
  "prod.minAmount": { en: "Minimum amount ({cur})", ms: "Jumlah minimum ({cur})", zh: "最低金额（{cur}）", ta: "குறைந்தபட்ச தொகை ({cur})", ar: "الحد الأدنى للمبلغ ({cur})" },
  "prod.maxAmount": { en: "Maximum amount ({cur}, optional)", ms: "Jumlah maksimum ({cur}, pilihan)", zh: "最高金额（{cur}，可选）", ta: "அதிகபட்ச தொகை ({cur}, விருப்பம்)", ar: "الحد الأقصى للمبلغ ({cur}، اختياري)" },
  "prod.suggestedAmount": { en: "Suggested amount ({cur}, optional)", ms: "Jumlah cadangan ({cur}, pilihan)", zh: "建议金额（{cur}，可选）", ta: "பரிந்துரைக்கப்பட்ட தொகை ({cur}, விருப்பம்)", ar: "المبلغ المقترح ({cur}، اختياري)" },
  "prod.amountHint": { en: "The buyer types the amount on the form. The server checks it against these limits before charging. Leave the maximum empty for no upper limit.", ms: "Pembeli menaip jumlah pada borang. Server menyemaknya terhadap had ini sebelum mengecaj — biarkan maksimum kosong untuk tiada had atas.", zh: "买家在表单上输入金额。服务器在收费前会按这些限制校验——最高金额留空表示不设上限。", ta: "வாங்குபவர் படிவத்தில் தொகையைத் தட்டச்சு செய்கிறார். கட்டணம் வசூலிக்கும் முன் சேவையகம் இந்த வரம்புகளுக்கு எதிராகச் சரிபார்க்கும் — உச்ச வரம்பு வேண்டாம் என்றால் அதிகபட்சத்தை காலியாக விடவும்.", ar: "يكتب المشتري المبلغ في النموذج، ويتحقق الخادم منه وفق هذه الحدود قبل التحصيل — اترك الحد الأقصى فارغًا لعدم وجود سقف." },
  "prod.amountFloor": { en: "Minimum {cur}1.00. Below that, the payment fee costs more than the gift.", ms: "Minimum {cur}1.00 — di bawah itu caj bayaran melebihi nilai derma.", zh: "最低 {cur}1.00——低于此数额时付款费用会超过捐款本身。", ta: "குறைந்தபட்சம் {cur}1.00 — அதற்குக் கீழே கட்டணம் நன்கொடையை விட அதிகமாகும்.", ar: "الحد الأدنى {cur}1.00 — أقل من ذلك تصبح رسوم الدفع أكبر من التبرع." },
  "prod.amountInvalid": { en: "Check the amounts: the minimum must be at least 1.00, and any maximum or suggested amount must not be below the minimum.", ms: "Semak jumlah: minimum mesti sekurang-kurangnya 1.00, dan maksimum atau cadangan tidak boleh di bawah minimum.", zh: "请检查金额：最低金额不得低于 1.00，最高金额或建议金额不得低于最低金额。", ta: "தொகைகளைச் சரிபார்க்கவும்: குறைந்தபட்சம் 1.00 ஆக இருக்க வேண்டும், அதிகபட்சம் அல்லது பரிந்துரை குறைந்தபட்சத்திற்குக் குறைவாக இருக்கக் கூடாது.", ar: "تحقق من المبالغ: يجب ألا يقل الحد الأدنى عن 1.00، ويجب ألا يقل الحد الأقصى أو المبلغ المقترح عن الحد الأدنى." },
  "prod.simpleStock": { en: "Stock (blank = ∞)", ms: "Stok (kosong = ∞)", zh: "库存（留空 = ∞）", ta: "சரக்கு (காலியாக = ∞)", ar: "المخزون (فارغ = ∞)" },
  "prod.simpleImages": { en: "Product images", ms: "Gambar produk", zh: "商品图片", ta: "தயாரிப்பு படங்கள்", ar: "صور المنتج" },
  "prod.saved": { en: "Product saved ✓", ms: "Produk disimpan ✓", zh: "商品已保存 ✓", ta: "தயாரிப்பு சேமிக்கப்பட்டது ✓", ar: "تم حفظ المنتج ✓" },
  "prod.saveFail": { en: "Failed to save product.", ms: "Gagal simpan produk.", zh: "保存商品失败。", ta: "தயாரிப்பைச் சேமிக்க முடியவில்லை.", ar: "فشل حفظ المنتج." },
  "prod.creating": { en: "Creating…", ms: "Mencipta…", zh: "创建中…", ta: "உருவாக்குகிறது…", ar: "جارٍ الإنشاء…" },
  "prod.saving": { en: "Saving…", ms: "Menyimpan…", zh: "保存中…", ta: "சேமிக்கிறது…", ar: "جارٍ الحفظ…" },
  "prod.createProduct": { en: "Create product", ms: "Cipta produk", zh: "创建商品", ta: "தயாரிப்பு உருவாக்கு", ar: "إنشاء منتج" },
  "prod.saveProduct": { en: "Save product", ms: "Simpan produk", zh: "保存商品", ta: "தயாரிப்பைச் சேமி", ar: "حفظ المنتج" },
  "prod.notFound": { en: "Product not found.", ms: "Produk tidak dijumpai.", zh: "未找到商品。", ta: "தயாரிப்பு கிடைக்கவில்லை.", ar: "المنتج غير موجود." },
  "prod.loadFail": { en: "Failed to load product.", ms: "Gagal muat produk.", zh: "加载商品失败。", ta: "தயாரிப்பை ஏற்ற முடியவில்லை.", ar: "فشل تحميل المنتج." },
  // Issue #551: shown when the price is not a usable amount (non-finite / out of
  // range), so a malformed price is refused with a visible reason instead of being
  // sent as null and silently stored as RM 0.00.
  "prod.priceInvalid": {
    en: "Enter an amount like 25.00 in {name}, with no letters or symbols.",
    ms: "Masukkan jumlah seperti 25.00 dalam {name}, tanpa huruf atau simbol.",
    zh: "请在 {name} 输入类似 25.00 的金额，不要包含字母或符号。",
    ta: "{name} இல் 25.00 போன்ற தொகையை உள்ளிடவும்; எழுத்துகளோ குறியீடுகளோ வேண்டாம்.",
    ar: "أدخل مبلغاً مثل 25.00 في {name}، دون حروف أو رموز.",
  },
  "prod.variationTypeNote": { en: "Variable products use per-variation price, stock and image. Product-level price/stock/images are ignored.", ms: "Produk variable guna harga, stok dan gambar per variasi. Harga/stok/gambar peringkat produk diabaikan.", zh: "多规格商品使用每个规格的价格、库存和图片。商品级价格/库存/图片将被忽略。", ta: "மாறுபடும் தயாரிப்புகள் ஒவ்வொரு மாறுபாட்டின் விலை, சரக்கு மற்றும் படத்தைப் பயன்படுத்துகின்றன. தயாரிப்பு நிலை விலை/சரக்கு/படங்கள் புறக்கணிக்கப்படும்.", ar: "تستخدم المنتجات المتغيرة سعر ومخزون وصورة كل خيار. تُتجاهل أسعار/مخزون/صور مستوى المنتج." },
  // Issue #623: per-variation subscription cycle. The empty field means
  // "inherit the product", which is why the hint is a placeholder and the reset
  // is an explicit empty option — the two states must stay distinguishable.
  "prod.variationCycle": { en: "Subscription cycle for this variation", ms: "Kitaran langganan untuk variasi ini", zh: "此规格的订阅周期", ta: "இந்த மாறுபாட்டின் சந்தா சுழற்சி", ar: "دورة الاشتراك لهذا الخيار" },
  "prod.variationCycleHint": { en: "Leave a field empty to use the product's value. Typing a value overrides it for this variation only.", ms: "Biarkan kosong untuk guna nilai produk. Isi nilai untuk ganti bagi variasi ini sahaja.", zh: "留空即使用商品的设置。填入值则仅针对此规格覆盖。", ta: "தயாரிப்பின் மதிப்பைப் பயன்படுத்த காலியாக விடவும். மதிப்பை உள்ளிட்டால் இந்த மாறுபாட்டிற்கு மட்டும் மாற்றும்.", ar: "اترك الحقل فارغًا لاستخدام قيمة المنتج. إدخال قيمة يستبدلها لهذا الخيار فقط." },
  "prod.inheritProduct": { en: "Use product ({value})", ms: "Ikut produk ({value})", zh: "跟随商品（{value}）", ta: "தயாரிப்பைப் பின்பற்று ({value})", ar: "استخدم المنتج ({value})" },
  "prod.variationDocs": { en: "Variation products documentation", ms: "Dokumentasi produk variasi", zh: "多规格商品文档", ta: "மாறுபாட்டு தயாரிப்புகள் ஆவணம்", ar: "وثائق المنتجات المتغيرة" },
  // issue #131: Subscriptions dashboard (list/filter/paginate/cancel)
  "sub.title": { en: "Subscriptions", ms: "Langganan", zh: "订阅", ta: "சந்தாக்கள்", ar: "الاشتراكات" },
  "sub.subtitle": { en: "Recurring memberships. Cancel any time and restart later. The saved card stays on file.", ms: "Langganan berulang pelanggan — batalkan tanpa hilang token kad mereka (boleh aktif semula).", zh: "循环客户订阅 — 取消不会丢失其卡片令牌（可重新激活）。", ta: "தொடர்ச்சியான வாடிக்கையாளர் சந்தாக்கள் — அட்டை டோக்கனை இழக்காமல் ரத்து செய்யலாம் (மீண்டும் செயல்படுத்தக்கூடியது).", ar: "اشتراكات العملاء المتكررة — يمكن الإلغاء دون فقدان رمز البطاقة (قابل لإعادة التنشيط)." },
  "sub.customer": { en: "Customer", ms: "Pelanggan", zh: "客户", ta: "வாடிக்கையாளர்", ar: "العميل" },
  "sub.product": { en: "Product", ms: "Produk", zh: "商品", ta: "தயாரிப்பு", ar: "المنتج" },
  "sub.amountPeriod": { en: "Amount / period", ms: "Jumlah / tempoh", zh: "金额 / 周期", ta: "தொகை / காலம்", ar: "المبلغ / الفترة" },
  "sub.nextCharge": { en: "Next charge", ms: "Caj seterusnya", zh: "下次扣费", ta: "அடுத்த கட்டணம்", ar: "الشحنة القادمة" },
  "sub.retryAttempts": { en: "Retry attempts", ms: "Cubaan ulang", zh: "重试次数", ta: "மீண்டும் முயற்சிகள்", ar: "محاولات إعادة المحاولة" },
  "sub.statusAll": { en: "All statuses", ms: "Semua status", zh: "所有状态", ta: "அனைத்து நிலைகள்", ar: "كل الحالات" },
  "sub.active": { en: "Active", ms: "Aktif", zh: "活跃", ta: "செயலில்", ar: "نشط" },
  "sub.pastDue": { en: "Past due", ms: "Lewat", zh: "逾期", ta: "தவறியது", ar: "متأخر" },
  "sub.cancelled": { en: "Cancelled", ms: "Dibatalkan", zh: "已取消", ta: "ரத்து செய்யப்பட்டது", ar: "ملغى" },
  "sub.completed": { en: "Completed", ms: "Selesai", zh: "已完成", ta: "நிறைவு", ar: "مكتمل" },
  "sub.cancel": { en: "Cancel", ms: "Batal", zh: "取消", ta: "ரத்து", ar: "إلغاء" },
  "sub.cancelConfirm": { en: "Cancel this subscription for {customer}? Their card token is retained, so they can be re-charged/re-activated later.", ms: "Batalkan langganan untuk {customer}? Token kad mereka kekal, boleh dicaj/diaktif semula nanti.", zh: "取消 {customer} 的订阅？其卡片令牌将保留，之后可重新扣费/激活。", ta: "{customer}-க்கான சந்தாவை ரத்து செய்யவா? அவர்களின் அட்டை டோக்கன் தக்கவைக்கப்படுகிறது, பின்னர் மீண்டும் கட்டணம்/செயல்படுத்தலாம்.", ar: "إلغاء الاشتراك لـ {customer}؟ سيتم الاحتفاظ برمز بطاقتهم، ويمكن إعادة الشحن/التنشيط لاحقاً." },
  "sub.empty": { en: "No subscriptions yet.", ms: "Belum ada langganan.", zh: "暂无订阅。", ta: "இன்னும் சந்தாக்கள் இல்லை.", ar: "لا توجد اشتراكات بعد." },
  "sub.cancelErr": { en: "Failed to cancel subscription.", ms: "Gagal batalkan langganan.", zh: "取消订阅失败。", ta: "சந்தாவை ரத்து செய்ய முடியவில.", ar: "فشل إلغاء الاشتراك." },
  "sub.tokenRetained": { en: "Token retained", ms: "Token kekal", zh: "令牌已保留", ta: "டோக்கன் தக்கவைக்கப்பட்டது", ar: "تم الاحتفاظ بالرمز" },
  "sub.filterStatus": { en: "Status filter", ms: "Tapis status", zh: "状态筛选", ta: "நிலை வடிப்பான்", ar: "تصفية الحالة" },
  "sub.countItems": { en: "{n} subscriptions", ms: "{n} langganan", zh: "{n} 个订阅", ta: "{n} சந்தாக்கள்", ar: "{n} اشتراك" },
  "sub.pageOf": { en: "page {page} of {total}", ms: "halaman {page} daripada {total}", zh: "第 {page} 页，共 {total} 页", ta: "பக்கம் {page} / {total}", ar: "صفحة {page} من {total}" },
  "sub.month": { en: "monthly", ms: "bulanan", zh: "每月", ta: "மாதந்தோறும்", ar: "شهرياً" },
  "sub.year": { en: "yearly", ms: "tahunan", zh: "每年", ta: "ஆண்டுதோறும்", ar: "سنوياً" },
  "sub.updateCard": { en: "Update card", ms: "Kemas kini kad", zh: "更新卡片", ta: "அட்டையைப் புதுப்பி", ar: "تحديث البطاقة" },
  "sub.updateCardConfirm": { en: "Send {customer} a secure link to authorize a new card? The subscription keeps its schedule; the old card token is retained but no longer used.", ms: "Hantar pautan selamat kepada {customer} untuk sahkan kad baharu? Langganan kekal pada jadual; token kad lama kekal tetapi tidak lagi digunakan.", zh: "向 {customer} 发送安全链接以授权新卡？订阅保持原计划；旧卡令牌保留但不再使用。", ta: "{customer}-க்கு புதிய அட்டையை அங்கீகரிக்க பாதுகாப்பான இணைப்பை அனுப்பவா? சந்தா அட்டவணையைத் தக்கவைக்கும்; பழைய அட்டை டோக்கன் தக்கவைக்கப்படும் ஆனால் இனி பயன்படுத்தப்படாது.", ar: "إرسال رابط آمن إلى {customer} للسماح ببطاقة جديدة؟ يحتفظ الاشتراك بجدوله؛ يتم الاحتفاظ برمز البطاقة القديم لكنه لم يعد مستخدماً." },
  "sub.updateCardErr": { en: "Failed to start card update.", ms: "Gagal mulakan kemas kini kad.", zh: "启动卡片更新失败。", ta: "அட்டை புதுப்பிப்பைத் தொடங்க முடியவில.", ar: "فشل بدء تحديث البطاقة." },
  "sub.cardNeedsUpdate": { en: "Card failed. Update the payment method", ms: "Kad gagal — kemas kini kaedah pembayaran", zh: "卡片失败 — 更新支付方式", ta: "அட்டை தோல்வி — கட்டண முறையைப் புதுப்பி", ar: "فشلت البطاقة — حدّث طريقة الدفع" },
  "sub.cardUpdatePaused": { en: "Auto-cancel paused while card update is pending", ms: "Auto-batal dijeda semasa kemas kini kad belum selesai", zh: "卡片更新待处理期间自动取消已暂停", ta: "அட்டை புதுப்பிப்பு நிலுவையில் இருக்கும்போது தானியங்கி ரத்து இடைநிறுத்தப்பட்டது", ar: "تم إيقاف الإلغاء التلقائي مؤقتاً أثناء انتظار تحديث البطاقة" },
  "sub.updateCardSent": { en: "Card update link sent. Ask the customer to complete it.", ms: "Pautan kemas kini kad dihantar. Minta pelanggan lengkapkannya.", zh: "卡片更新链接已发送。请让客户完成。", ta: "அட்டை புதுப்பிப்பு இணைப்பு அனுப்பப்பட்டது. வாடிக்கையாளரை முடிக்கச் சொல்லுங்கள்.", ar: "تم إرسال رابط تحديث البطاقة. اطلب من العميل إكماله." },
  // forms page
  "forms.title": { en: "Forms", ms: "Borang", zh: "表单", ta: "படிவங்கள்", ar: "النماذج" },
  "forms.subtitle": { en: "Your published and draft forms", ms: "Borang live & draft anda", zh: "您发布的订单表单", ta: "வெளியிடப்பட்ட ஆர்டர் படிவங்கள்", ar: "نماذج الطلبات المنشورة" },
  "forms.new": { en: "New form", ms: "Borang Baru", zh: "+ 新表单", ta: "+ புதிய படிவம்", ar: "+ نموذج جديد" },
  "forms.empty": { en: "No forms yet. Create your first one.", ms: "Belum ada borang — bina yang pertama.", zh: "还没有表单。", ta: "இன்னும் படிவங்கள் இல்லை.", ar: "لا توجد نماذج بعد." },
  "forms.live": { en: "LIVE", ms: "LIVE", zh: "已上线", ta: "நேரலையில்", ar: "منشور" },
  "forms.draft": { en: "Draft", ms: "Draft", zh: "草稿", ta: "வரைவு", ar: "مسودة" },
  // Card t_983425e5: a PUBLISHED payment form whose buyer page says "This form is
  // not accepting orders right now" must not wear a clean green LIVE pill. The
  // badge names the buyer-visible consequence; the hint says why (and is the
  // `title`/`aria-label` so the reason is reachable without a colour).
  "forms.notAcceptingOrders": { en: "Not accepting orders", ms: "Tidak menerima pesanan", zh: "不接受订单", ta: "ஆர்டர்களை ஏற்கவில்லை", ar: "لا يستقبل الطلبات" },
  "forms.notAcceptingHint": { en: "Buyers see \u201cThis form is not accepting orders right now\u201d \u2014 no product is selected for this form.", ms: "Pembeli melihat \u201cBorang ini tidak menerima pesanan buat masa ini\u201d \u2014 tiada produk dipilih untuk borang ini.", zh: "\u4e70\u5bb6\u4f1a\u770b\u5230\u201c\u6b64\u8868\u5355\u76ee\u524d\u4e0d\u63a5\u53d7\u8ba2\u5355\u3002\u201d\u2014\u2014\u6b64\u8868\u5355\u5c1a\u672a\u9009\u62e9\u5546\u54c1\u3002", ta: "\u0bb5\u0bbe\u0b99\u0bcd\u0b95\u0bc1\u0baa\u0bb5\u0bb0\u0bcd\u0b95\u0bb3\u0bcd \u201c\u0b87\u0ba8\u0bcd\u0ba4\u0baa\u0bcd \u0baa\u0b9f\u0bbf\u0bb5\u0bae\u0bcd \u0b87\u0baa\u0bcd\u0baa\u0bcb\u0ba4\u0bc1 \u0b86\u0bb0\u0bcd\u0b9f\u0bb0\u0bcd\u0b95\u0bb3\u0bc8 \u0b8f\u0bb1\u0bcd\u0b95\u0bb5\u0bbf\u0bb2\u0bcd\u0bb2\u0bc8.\u201d \u0b8e\u0ba9\u0bcd\u0bb1\u0bc1 \u0b95\u0bbe\u0ba3\u0bcd\u0baa\u0bbe\u0bb0\u0bcd\u0b95\u0bcd\u0b95\u0bbf\u0bb1\u0bbe\u0bb0\u0bcd\u0b95\u0bb3\u0bcd \u2014 \u0b87\u0ba8\u0bcd\u0ba4\u0baa\u0bcd \u0baa\u0b9f\u0bbf\u0bb5\u0ba4\u0bcd\u0ba4\u0bbf\u0bb1\u0bcd\u0b95\u0bc1 \u0baa\u0bca\u0bb0\u0bc1\u0bb3\u0bcd \u0ba4\u0bc7\u0bb0\u0bcd\u0ba8\u0bcd\u0ba4\u0bc6\u0b9f\u0bc1\u0b95\u0bcd\u0b95\u0baa\u0bcd\u0baa\u0b9f\u0bb5\u0bbf\u0bb2\u0bcd\u0bb2\u0bc8.", ar: "\u064a\u0631\u0649 \u0627\u0644\u0645\u0634\u062a\u0631\u064a \u00ab\u0647\u0630\u0627 \u0627\u0644\u0646\u0645\u0648\u0630\u062c \u0644\u0627 \u064a\u0633\u062a\u0642\u0628\u0644 \u0627\u0644\u0637\u0644\u0628\u0627\u062a \u0627\u0644\u0622\u0646.\u00bb \u2014 \u0644\u0645 \u064a\u062a\u0645 \u0627\u062e\u062a\u064a\u0627\u0631 \u0645\u0646\u062a\u062c \u0644\u0647\u0630\u0627 \u0627\u0644\u0646\u0645\u0648\u0630\u062c." },
  "forms.private": { en: "Private", ms: "Private", zh: "私密", ta: "தனிப்பட்ட", ar: "خاص" },
  "forms.unlisted": { en: "Unlisted", ms: "Tidak disenaraikan", zh: "未列出", ta: "பட்டியலிடப்படாதது", ar: "غير مدرج" },
  // A form with no resolvable public host has no link to show. The row must say
  // so instead of rendering a link to nowhere (an empty href reloads /forms).
  "forms.urlUnavailable": { en: "No public link yet", ms: "Belum ada pautan awam", zh: "暂无公开链接", ta: "பொது இணைப்பு இன்னும் இல்லை", ar: "لا يوجد رابط عام بعد" },
  "forms.viewUnavailableHint": { en: "No public link yet. Publish the form, or set a workspace or custom domain first", ms: "Belum ada pautan awam — terbitkan borang, atau tetapkan domain tersuai dahulu", zh: "暂无公开链接 — 请先发布表单或设置自定义域名", ta: "பொது இணைப்பு இல்லை — படிவத்தை வெளியிடவும் அல்லது தனிப்பயன் டொமைன் அமைக்கவும்", ar: "لا يوجد رابط عام بعد — انشر النموذج أو اضبط نطاقًا مخصصًا أولاً" },
  "forms.edit": { en: "Edit", ms: "Edit", zh: "编辑", ta: "திருத்து", ar: "تحرير" },
  "forms.delete": { en: "Delete", ms: "Buang", zh: "删除", ta: "நீக்கு", ar: "حذف" },
  "forms.selectAll": { en: "Select all", ms: "Pilih semua", zh: "全选", ta: "அனைத்தையும் தேர்ந்தெடு", ar: "تحديد الكل" },
  "forms.nSelected": { en: "{n} selected", ms: "{n} dipilih", zh: "已选 {n} 项", ta: "{n} தேர்ந்தெடுக்கப்பட்டது", ar: "تم اختيار {n}" },
  // the two per-row checkbox labels were hardcoded English
  // ("Select all forms" / `Select ${title}`) on a fully i18n page, so a
  // screen-reader user on ms/zh/ta/ar heard English. Keyed so the accessible
  // name is translated like every other string on this page.
  "forms.selectAllAria": { en: "Select all forms", ms: "Pilih semua borang", zh: "选择所有表单", ta: "அனைத்து படிவங்களையும் தேர்ந்தெடு", ar: "تحديد كل النماذج" },
  "forms.selectOneAria": { en: "Select {title}", ms: "Pilih {title}", zh: "选择 {title}", ta: "{title} தேர்ந்தெடு", ar: "تحديد {title}" },
  "forms.bulkDeleteConfirm": { en: "Move {n} form(s) to trash? They will be soft-deleted (hidden from the dashboard and public pages) and can be restored later.", ms: "Pindahkan {n} borang ke tong sampah? Ia akan dihapus lembut (tersembunyi dari dashboard & halaman awam) dan boleh dipulihkan kemudian.", zh: "将 {n} 个表单移入回收站？它们将被软删除（从仪表板和公开页面隐藏），之后可以恢复。", ta: "{n} படிவங்களை குப்பைக்கு நகர்த்தவா? அவை மென்-நீக்கம் செய்யப்படும் (டாஷ்போர்டு மற்றும் பொது பக்கங்களில் மறைக்கப்படும்), பின்னர் மீட்டெடுக்கலாம்.", ar: "نقل {n} نموذج (نماذج) إلى سلة المهملات؟ ستُحذف حذفاً ناعماً (تختفي من لوحة التحكم والصفحات العامة) ويمكن استعادتها لاحقاً." },
  // issue #174: template picker modal
  "tpl.title": { en: "Explore form templates", ms: "Terokai template borang", zh: "浏览表单模板", ta: "படிவ வார்ப்புருக்களை ஆராயுங்கள்", ar: "استكشف قوالب النماذج" },
  "tpl.subtitle": { en: "Start from a template or a blank form.", ms: "Mula dari template atau borang kosong.", zh: "从模板或空白表单开始。", ta: "வார்ப்புரு அல்லது வெற்று படிவத்திலிருந்து தொடங்குங்கள்.", ar: "ابدأ من قالب أو نموذج فارغ." },
  "tpl.blank": { en: "Blank form", ms: "Borang kosong", zh: "空白表单", ta: "வெற்று படிவம்", ar: "نموذج فارغ" },
  // Card t_bb0d0bee: the blank card's own call to action — it had none while
  // every template card had "Use template".
  "tpl.blankCta": { en: "Start blank", ms: "Mula kosong", zh: "从空白开始", ta: "வெற்றிலிருந்து தொடங்கு", ar: "ابدأ فارغاً" },
  "tpl.blankHint": { en: "Start from scratch", ms: "Mula dari kosong", zh: "从零开始", ta: "புதிதாகத் தொடங்கு", ar: "ابدأ من الصفر" },
  "tpl.use": { en: "Use template", ms: "Guna template", zh: "使用模板", ta: "வார்ப்புருவைப் பயன்படுத்து", ar: "استخدم القالب" },
  "tpl.using": { en: "Creating…", ms: "Mencipta…", zh: "创建中…", ta: "உருவாக்குகிறது…", ar: "جارٍ الإنشاء…" },
  "tpl.useFail": { en: "Failed to use template.", ms: "Gagal guna template.", zh: "使用模板失败。", ta: "வார்ப்புருவைப் பயன்படுத்த முடியவில்லை.", ar: "فشل استخدام القالب." },
  "tpl.empty": { en: "No templates available.", ms: "Tiada template tersedia.", zh: "暂无可用模板。", ta: "வார்ப்புருக்கள் இல்லை.", ar: "لا توجد قوالب متاحة." },
  "tpl.close": { en: "Close", ms: "Tutup", zh: "关闭", ta: "மூடு", ar: "إغلاق" },
  // issue #457: merchant-managed templates (save/edit/delete own templates)
  "nav.templates": { en: "Templates", ms: "Template", zh: "模板", ta: "வார்ப்புருக்கள்", ar: "القوالب" },
  "tplmg.title": { en: "Templates", ms: "Template", zh: "模板", ta: "வார்ப்புருக்கள்", ar: "القوالب" },
  "tplmg.subtitle": {
    en: "Save a form as a reusable template, then start new forms from it.",
    ms: "Simpan borang sebagai template boleh guna semula, kemudian mula borang baharu daripadanya.",
    zh: "将表单保存为可重复使用的模板，然后从模板创建新表单。",
    ta: "ஒரு படிவத்தை மீண்டும் பயன்படுத்தக்கூடிய வார்ப்புருவாகச் சேமித்து, அதிலிருந்து புதிய படிவங்களைத் தொடங்குங்கள்.",
    ar: "احفظ نموذجاً كقالب قابل لإعادة الاستخدام، ثم ابدأ نماذج جديدة منه.",
  },
  "tplmg.new": { en: "New template", ms: "Template baharu", zh: "新建模板", ta: "புதிய வார்ப்புரு", ar: "قالب جديد" },
  "tplmg.newTitle": { en: "Untitled template", ms: "Template tanpa tajuk", zh: "未命名模板", ta: "தலைப்பற்ற வார்ப்புரு", ar: "قالب بدون عنوان" },
  "tplmg.saveFromForm": { en: "Save form as template", ms: "Simpan borang sebagai template", zh: "将表单保存为模板", ta: "படிவத்தை வார்ப்புருவாகச் சேமி", ar: "احفظ النموذج كقالب" },
  "tplmg.pickForm": { en: "Which form?", ms: "Borang mana?", zh: "选择表单？", ta: "எந்தப் படிவம்?", ar: "أي نموذج؟" },
  "tplmg.pickFormHint": {
    en: "Its fields and design are copied into a reusable template.",
    ms: "Medan dan reka bentuknya disalin ke dalam template boleh guna semula.",
    zh: "其字段和设计将复制到可重复使用的模板中。",
    ta: "அதன் புலங்களும் வடிவமைப்பும் மீண்டும் பயன்படுத்தக்கூடிய வார்ப்புருவில் நகலெடுக்கப்படும்.",
    ar: "تُنسخ حقوله وتصميمه إلى قالب قابل لإعادة الاستخدام.",
  },
  "tplmg.saveThis": { en: "Save", ms: "Simpan", zh: "保存", ta: "சேமி", ar: "حفظ" },
  "tplmg.noForms": { en: "You have no forms yet.", ms: "Anda belum ada borang.", zh: "您还没有表单。", ta: "உங்களிடம் இன்னும் படிவங்கள் இல்லை.", ar: "ليس لديك نماذج بعد." },
  "tplmg.saved": { en: "Template saved.", ms: "Template disimpan.", zh: "模板已保存。", ta: "வார்ப்புரு சேமிக்கப்பட்டது.", ar: "تم حفظ القالب." },
  "tplmg.saveFail": { en: "Failed to save template.", ms: "Gagal simpan template.", zh: "保存模板失败。", ta: "வார்ப்புருவைச் சேமிக்க முடியவில்லை.", ar: "فشل حفظ القالب." },
  "tplmg.deleteConfirm": {
    en: "Delete this template? Forms created from it are not affected.",
    ms: "Padam template ini? Borang yang dicipta daripadanya tidak terjejas.",
    zh: "删除此模板？由此创建的表单不受影响。",
    ta: "இந்த வார்ப்புருவை நீக்கவா? இதிலிருந்து உருவாக்கப்பட்ட படிவங்கள் பாதிக்கப்படாது.",
    ar: "حذف هذا القالب؟ لن تتأثر النماذج المنشأة منه.",
  },
  "tplmg.deleteFail": { en: "Failed to delete template.", ms: "Gagal padam template.", zh: "删除模板失败。", ta: "வார்ப்புருவை நீக்க முடியவில்லை.", ar: "فشل حذف القالب." },
  "tplmg.use": { en: "Use template", ms: "Guna template", zh: "使用模板", ta: "வார்ப்புருவைப் பயன்படுத்து", ar: "استخدم القالب" },
  "tplmg.useFail": { en: "Failed to use template.", ms: "Gagal guna template.", zh: "使用模板失败。", ta: "வார்ப்புருவைப் பயன்படுத்த முடியவில்லை.", ar: "فشل استخدام القالب." },
  "tplmg.rename": { en: "Rename", ms: "Namakan semula", zh: "重命名", ta: "மறுபெயரிடு", ar: "إعادة تسمية" },
  // Issue #457 follow-up (Wan, 2026-09-19): edit one of your own templates in
  // the builder — its FIELDS and DESIGN, which the Templates page could not
  // reach before (only Use template / Rename / Delete existed).
  "tplmg.edit": { en: "Edit", ms: "Edit", zh: "编辑", ta: "திருத்து", ar: "تحرير" },
  "tplmg.editHint": { en: "Edit this template's fields and design", ms: "Edit medan dan reka bentuk template ini", zh: "编辑此模板的字段和设计", ta: "இந்த வார்ப்புருவின் புலங்களையும் வடிவமைப்பையும் திருத்து", ar: "حرّر حقول هذا القالب وتصميمه" },
  "tplmg.editLabel": { en: "Template", ms: "Template", zh: "模板", ta: "வார்ப்புரு", ar: "القالب" },
  "tplmg.editTitle": { en: "Template title", ms: "Tajuk template", zh: "模板标题", ta: "வார்ப்புரு தலைப்பு", ar: "عنوان القالب" },
  "tplmg.editSaved": {
    en: "Template saved. Forms already created from it are not affected.",
    ms: "Template disimpan. Borang yang sudah dicipta daripadanya tidak terjejas.",
    zh: "模板已保存。已由此创建的表单不受影响。",
    ta: "வார்ப்புரு சேமிக்கப்பட்டது. ஏற்கனவே அதிலிருந்து உருவாக்கப்பட்ட படிவங்கள் பாதிக்கப்படாது.",
    ar: "تم حفظ القالب. النماذج المنشأة منه بالفعل لن تتأثر.",
  },
  "tplmg.editFail": { en: "Failed to save the template.", ms: "Gagal simpan template.", zh: "保存模板失败。", ta: "வார்ப்புருவைச் சேமிக்க முடியவில்லை.", ar: "فشل حفظ القالب." },
  "tplmg.editLoadFail": { en: "Failed to load the template.", ms: "Gagal muat template.", zh: "加载模板失败。", ta: "வார்ப்புருவை ஏற்ற முடியவில்லை.", ar: "فشل تحميل القالب." },
  "tplmg.save": { en: "Save", ms: "Simpan", zh: "保存", ta: "சேமி", ar: "حفظ" },
  "tplmg.cancel": { en: "Cancel", ms: "Batal", zh: "取消", ta: "ரத்துசெய்", ar: "إلغاء" },
  "tplmg.docs": { en: "Templates documentation", ms: "Dokumentasi template", zh: "模板文档", ta: "வார்ப்புரு ஆவணங்கள்", ar: "وثائق القوالب" },
  "tplmg.empty": {
    en: "No templates yet. Save a form as a template to reuse it later.",
    ms: "Belum ada template. Simpan borang sebagai template untuk guna semula kemudian.",
    zh: "暂无模板。将表单保存为模板以便日后重复使用。",
    ta: "இன்னும் வார்ப்புருக்கள் இல்லை. பின்னர் மீண்டும் பயன்படுத்த ஒரு படிவத்தை வார்ப்புருவாகச் சேமிக்கவும்.",
    ar: "لا توجد قوالب بعد. احفظ نموذجاً كقالب لإعادة استخدامه لاحقاً.",
  },
  // builder: save the current form as a reusable template (issue #457)
  "bld.saveAsTemplate": { en: "Save as template", ms: "Simpan sebagai template", zh: "保存为模板", ta: "வார்ப்புருவாகச் சேமி", ar: "حفظ كقالب" },
  "bld.saveAsTemplateDone": { en: "Saved as a template.", ms: "Disimpan sebagai template.", zh: "已保存为模板。", ta: "வார்ப்புருவாகச் சேமிக்கப்பட்டது.", ar: "تم الحفظ كقالب." },
  "bld.saveAsTemplateFail": { en: "Failed to save as template.", ms: "Gagal simpan sebagai template.", zh: "保存为模板失败。", ta: "வார்ப்புருவாகச் சேமிக்க முடியவில்லை.", ar: "فشل الحفظ كقالب." },
  // builder common
  "builder.publish": { en: "Publish", ms: "Publish", zh: "发布", ta: "வெளியிடு", ar: "نشر" },
  "builder.savePub": { en: "Save & publish", ms: "Simpan & Publish", zh: "保存并发布", ta: "சேமித்து வெளியிடு", ar: "حفظ ونشر" },
  "builder.saving": { en: "Saving…", ms: "Menyimpan…", zh: "保存中…", ta: "சேமிக்கிறது…", ar: "جارٍ الحفظ…" },
  "builder.unsaved": { en: "unsaved", ms: "belum disimpan", zh: "未保存", ta: "சேமிக்கப்படவில்லை", ar: "غير محفوظ" },
  // settings
  "set.title": { en: "Settings", ms: "Tetapan", zh: "设置", ta: "அமைப்புகள்", ar: "الإعدادات" },
  "set.chipTitle": { en: "CHIP Collect", ms: "CHIP Collect", zh: "CHIP Collect", ta: "CHIP Collect", ar: "CHIP Collect" },
  "set.getYour": { en: "Get your", ms: "Dapatkan", zh: "获取您的", ta: "பெறுங்கள்", ar: "احصل على" },
  "set.and": { en: "&", ms: "&", zh: "和", ta: "&", ar: "&" },
  "set.from": { en: "from", ms: "dari", zh: "来自", ta: "இருந்து", ar: "من" },
  "set.chipPortal": { en: "CHIP Merchant Portal → Developers", ms: "Portal Pedagang CHIP → Developers", zh: "CHIP 商户门户 → 开发者", ta: "CHIP வணிகர் தளம் → Developers", ar: "بوابة تجار CHIP → المطورين" },
  "set.keysEncrypted": { en: "Keys are stored encrypted and never shown again.", ms: "Kunci disimpan terenkrip — tidak dipaparkan lagi.", zh: "密钥加密存储 — 不会再次显示。", ta: "விசைகள் என்க்ரிப்ட் செய்யப்பட்டு சேமிக்கப்படும் — மீண்டும் காட்டப்படாது.", ar: "تُخزَّن المفاتيح مشفرة — لا تُعرض مرة أخرى." },
  // Card t_9efc5ed7: the Test/Live MODE control is gone. `chip_mode` never chose
  // the API endpoint (ChipAPIBaseUrl does), never blocked anything and never
  // changed a payment, so the merchant was answering a question nobody read. The
  // keys that existed only to ask it — set.mode, set.modeHint, set.live, set.test,
  // set.activeMode, set.apiVerified, set.notSet, set.secretMasked,
  // set.secretMaskedPlaceholder — are deleted with the control; none of them has
  // a call site any more (src/lib/chipSettingsRedesign.test.ts pins that).
  //
  // What replaces them: ONE connection pill in the card's header, three truthful
  // states, and the actions the card now carries.
  "set.secretKey": { en: "Secret Key", ms: "Kunci Rahsia", zh: "密钥", ta: "ரகசிய விசை", ar: "المفتاح السري" },
  "set.connected": { en: "Connected", ms: "Disambung", zh: "已连接", ta: "இணைக்கப்பட்டது", ar: "متصل" },
  "set.notConnected": { en: "Not connected", ms: "Tidak disambung", zh: "未连接", ta: "இணைக்கப்படவில்லை", ar: "غير متصل" },
  "set.notVerified": { en: "Not verified", ms: "Tidak disahkan", zh: "未验证", ta: "சரிபார்க்கப்படவில்லை", ar: "غير موثّق" },
  // The placeholders describe the ACTION. Neither carries an ellipsis character:
  // the old `e.g. 5f2b…-uuid` shipped a literal "…" in the string and read as a
  // field that had been truncated before the merchant typed anything.
  "set.brandIdPh": { en: "Paste the Brand ID from the CHIP portal", ms: "Tampal Brand ID dari portal CHIP", zh: "从 CHIP 门户粘贴 Brand ID", ta: "CHIP தளத்திலிருந்து Brand ID ஐ ஒட்டவும்", ar: "الصق Brand ID من بوابة CHIP" },
  "set.secretKeyPh": { en: "Paste the Secret Key from the CHIP portal", ms: "Tampal Kunci Rahsia dari portal CHIP", zh: "从 CHIP 门户粘贴密钥", ta: "CHIP தளத்திலிருந்து ரகசிய விசையை ஒட்டவும்", ar: "الصق المفتاح السري من بوابة CHIP" },
  "set.savedEncryptedEnds": { en: "Saved encrypted · ends {tail}", ms: "Disimpan disulitkan · berakhir {tail}", zh: "已加密保存 · 结尾 {tail}", ta: "என்க்ரிப்ட் செய்து சேமிக்கப்பட்டது · முடிவு {tail}", ar: "محفوظ مشفراً · ينتهي بـ {tail}" },
  "set.savedEncrypted": { en: "Saved encrypted", ms: "Disimpan disulitkan", zh: "已加密保存", ta: "என்க்ரிப்ட் செய்து சேமிக்கப்பட்டது", ar: "محفوظ مشفراً" },
  "set.replace": { en: "Replace", ms: "Ganti", zh: "更换", ta: "மாற்று", ar: "استبدال" },
  "set.keepSaved": { en: "Keep saved key", ms: "Kekalkan kunci tersimpan", zh: "保留已存密钥", ta: "சேமித்த விசையை வைத்திரு", ar: "الاحتفاظ بالمفتاح المحفوظ" },
  "set.advanced": { en: "Advanced", ms: "Lanjutan", zh: "高级", ta: "மேம்பட்டவை", ar: "متقدم" },
  "set.testConnection": { en: "Test connection", ms: "Uji sambungan", zh: "测试连接", ta: "இணைப்பைச் சோதி", ar: "اختبار الاتصال" },
  "set.testing": { en: "Testing", ms: "Menguji", zh: "测试中", ta: "சோதிக்கிறது", ar: "جارٍ الاختبار" },
  "set.testConnectionOk": { en: "Connection OK. CHIP accepted these credentials.", ms: "Sambungan OK — CHIP menerima kredensial ini.", zh: "连接正常 — CHIP 已接受此凭证。", ta: "இணைப்பு சரி — CHIP இந்தச் சான்றுகளை ஏற்றுக்கொண்டது.", ar: "الاتصال سليم — قبل CHIP هذه البيانات." },
  "set.testConnectionFailed": { en: "Could not verify with CHIP", ms: "Tidak dapat mengesahkan dengan CHIP", zh: "无法通过 CHIP 验证", ta: "CHIP உடன் சரிபார்க்க முடியவில்லை", ar: "تعذر التحقق مع CHIP" },

  "set.emailFallback": { en: "Email fallback", ms: "Emel sandaran", zh: "备用邮箱", ta: "மாற்று மின்னஞ்சல்", ar: "البريد الإلكتروني الاحتياطي" },
  "set.emailFallbackHint": { en: "(optional; used when buyer email is missing; one-time forms only)", ms: "(pilihan — digunakan bila emel pembeli tiada; borang one-time sahaja)", zh: "（可选 — 买家邮箱缺失时使用；仅一次性表单）", ta: "(விருப்பம் — வாங்குபவர் மின்னஞ்சல் இல்லாதபோது பயன்படும்; ஒரு முறை படிவங்களுக்கு மட்டும்)", ar: "(اختياري — يُستخدم عند غياب بريد المشتري؛ النماذج لمرة واحدة فقط)" },
  "set.paymentMethods": { en: "Payment methods", ms: "Kaedah bayaran", zh: "支付方式", ta: "பணம் செலுத்தும் முறைகள்", ar: "طرق الدفع" },
  "set.allMethods": { en: "(blank = all available)", ms: "(kosong = semua tersedia)", zh: "（留空 = 全部可用）", ta: "(காலியாக = அனைத்தும் கிடைக்கும்)", ar: "(فارغ = جميع الطرق متاحة)" },
  // Card t_f11d676f: the payment-method control is a CHECKBOX GROUP now (it
  // used to be twelve rounded pills with no tick, which read as the filter chips
  // on /sales). Twelve checkboxes on a phone need a one-tap way through them, so
  // the group carries one toggle that always names the opposite of the current
  // state: "Select all" until every method is picked, then "Clear all".
  "set.pmSelectAll": { en: "Select all", ms: "Pilih semua", zh: "全选", ta: "அனைத்தையும் தேர்ந்தெடு", ar: "تحديد الكل" },
  "set.pmClearAll": { en: "Clear all", ms: "Kosongkan semua", zh: "清除全部", ta: "அனைத்தையும் அழி", ar: "مسح الكل" },
  // Card t_f11d676f: the settings tab strip is one scrolling row on a phone, so
  // it needs a control (not only a fade) that says "there are more tabs this
  // way". Each arrow names the direction it moves, so the pair is not two
  // identically-named buttons to a screen reader.
  "set.tabsScrollBack": { en: "Scroll tabs back", ms: "Tatal tab ke belakang", zh: "向后滚动标签", ta: "தாவல்களை பின்னால் உருட்டு", ar: "تمرير التبويبات للخلف" },
  "set.tabsScrollForward": { en: "Scroll tabs forward", ms: "Tatal tab ke hadapan", zh: "向前滚动标签", ta: "தாவல்களை முன்னால் உருட்டு", ar: "تمرير التبويبات للأمام" },
  "set.save": { en: "Save", ms: "Simpan", zh: "保存", ta: "சேமி", ar: "حفظ" },
  "set.clear": { en: "Clear", ms: "Kosongkan", zh: "清除", ta: "அழி", ar: "مسح" },
  "set.savedCreds": { en: "Saved. Credentials verified with CHIP ✓", ms: "Disimpan — kredensial disahkan dengan CHIP. ✓", zh: "已保存 — 凭证已通过 CHIP 验证 ✓", ta: "சேமிக்கப்பட்டது — CHIP உடன் சான்றுகள் சரிபார்க்கப்பட்டன ✓", ar: "تم الحفظ — تم التحقق من البيانات مع CHIP ✓" },
  "set.savedCredsUnverified": { en: "Saved. Credentials stored, but the CHIP check could not be confirmed.", ms: "Disimpan — kredensial disimpan, tetapi semakan CHIP tidak dapat disahkan.", zh: "已保存 — 凭证已存储，但无法确认 CHIP 验证。", ta: "சேமிக்கப்பட்டது — சான்றுகள் சேமிக்கப்பட்டன, ஆனால் CHIP சரிபார்ப்பை உறுதிப்படுத்த முடியவில்லை.", ar: "تم الحفظ — تم تخزين البيانات، لكن تعذر تأكيد التحقق من CHIP." },
  "set.failedSave": { en: "Failed to save", ms: "Gagal simpan", zh: "保存失败", ta: "சேமிப்பதில் தோல்வி", ar: "فشل الحفظ" },
  "set.credsCleared": { en: "CHIP credentials cleared.", ms: "Kredensial CHIP dikosongkan.", zh: "已清除 CHIP 凭证。", ta: "CHIP சான்றுகள் அழிக்கப்பட்டன.", ar: "تم مسح بيانات CHIP." },
  "set.domainsTitle": { en: "Custom domains", ms: "Domain tersuai", zh: "自定义域名", ta: "தனிப்பயன் டொமைன்கள்", ar: "النطاقات المخصصة" },
  "set.domainsHelp": { en: `Put your forms on your own domain, like shop.yourname.com. Add the domain below and we'll guide you through the 2 quick steps to connect it. Once connected, your forms go live on your domain automatically.`, ms: `Letakkan borang anda di domain anda sendiri — contohnya shop.yourname.com. Tambah domain di bawah dan kami akan bimbing anda melalui 2 langkah mudah untuk menyambungkannya. Setelah disambung, borang anda akan mula berjalan di domain anda secara automatik.`, zh: `将您的表单放在您自己的域名上 — 例如 shop.yourname.com。在下方添加域名，我们将引导您完成 2 个快速步骤来连接它。连接后，您的表单将自动在您的域名上上线。`, ta: `உங்கள் படிவங்களை உங்கள் சொந்த டொமைனில் வைக்கவும் — எடுத்துக்காட்டாக shop.yourname.com. கீழே டொமைனைச் சேர்க்கவும், அதை இணைக்க 2 எளிய படிகளில் நாங்கள் உங்களை வழிநடத்துவோம். இணைத்தவுடன், உங்கள் படிவங்கள் உங்கள் டொமைனில் தானாக நேரடியாகச் செயல்படும்.`, ar: `ضع نماذجك على نطاقك الخاص — مثل shop.yourname.com. أضف النطاق أدناه وسنرشدك خلال خطوتين سريعتين لربطه. بمجرد الربط، ستعمل نماذجك على نطاقك تلقائياً.` },
  "set.addDomain": { en: "Add domain", ms: "Tambah domain", zh: "添加域名", ta: "டொமைன் சேர்க்க", ar: "إضافة نطاق" },
  "set.domainAdded": { en: `{domain} added. Add CNAME {domain} → {target}, then Verify.`, ms: `{domain} ditambah. Tambah CNAME {domain} → {target}, kemudian Verify.`, zh: `{domain} 已添加。添加 CNAME {domain} → {target}，然后验证。`, ta: `{domain} சேர்க்கப்பட்டது. CNAME {domain} → {target} சேர்த்து, பின் சரிபார்க்கவும்.`, ar: `تمت إضافة {domain}. أضف CNAME {domain} → {target}، ثم تحقق.` },
  "set.failedAddDomain": { en: "Failed to add domain", ms: "Gagal tambah domain", zh: "添加域名失败", ta: "டொமைன் சேர்க்க தோல்வி", ar: "فشل إضافة النطاق" },
  "set.domainVerified": { en: "{domain} verified ✓", ms: "{domain} disahkan ✓", zh: "{domain} 已验证 ✓", ta: "{domain} சரிபார்க்கப்பட்டது ✓", ar: "{domain} تم التحقق ✓" },
  "set.verifyFailed": { en: "{domain}: verify failed", ms: "{domain}: pengesahan gagal", zh: "{domain}：验证失败", ta: "{domain}: சரிபார்ப்பு தோல்வி", ar: "{domain}: فشل التحقق" },
  "set.verifyFailDetail": { en: "Domain is not pointing to JomForm. Set a CNAME from your domain to the target below (or use Cloudflare proxy), then wait for DNS to propagate and Verify again.", ms: "Domain tidak menghala ke JomForm. Set CNAME dari domain anda ke sasaran di bawah (atau gunakan Cloudflare proxy), kemudian tunggu DNS propagate dan Verify semula.", zh: "域名未指向 JomForm。请将您的域名 CNAME 指向下方显示的目标（或使用 Cloudflare 代理），然后等待 DNS 生效并重新验证。", ta: "டொமைன் JomForm-ஐ சுட்டவில்லை. உங்கள் டொமைனில் இருந்து கீழே காட்டப்படும் இலக்குக்கு CNAME அமைக்கவும் (அல்லது Cloudflare proxy பயன்படுத்தவும்), பின் DNS பரவ காத்திருந்து மீண்டும் சரிபார்க்கவும்.", ar: "النطاق لا يشير إلى JomForm. عيّن CNAME من نطاقك إلى الهدف أدناه (أو استخدم وكيل Cloudflare)، ثم انتظر انتشار DNS وتحقق مرة أخرى." },
  "set.verified": { en: "verified ✓", ms: "disahkan ✓", zh: "已验证 ✓", ta: "சரிபார்க்கப்பட்டது ✓", ar: "تم التحقق ✓" },
  "set.pendingDns": { en: `pending DNS: CNAME to {target}, then Verify`, ms: `menunggu DNS — CNAME ke {target}, kemudian Verify`, zh: `等待 DNS — CNAME 到 {target}，然后验证`, ta: `DNS நிலுவையில் — {target}-க்கு CNAME, பின் சரிபார்க்கவும்`, ar: `بانتظار DNS — CNAME إلى {target} ثم تحقق` },
  "set.verify": { en: "Verify", ms: "Verify", zh: "验证", ta: "சரிபார்", ar: "تحقق" },
  "set.verifyCta": { en: "I have set the CNAME. Verify", ms: "Saya dah set CNAME — Verify", zh: "我已设置 CNAME — 验证", ta: "CNAME அமைத்துவிட்டேன் — சரிபார்", ar: "لقد عيّنت CNAME — تحقق" },
  "set.cnameRow": { en: "CNAME record", ms: "Rekod CNAME", zh: "CNAME 记录", ta: "CNAME பதிவு", ar: "سجل CNAME" },
  "set.cnameTarget": { en: "Points to", ms: "Menunjuk ke", zh: "指向", ta: "சுட்டும்", ar: "يشير إلى" },
  "set.liveLink": { en: "Live. Open https://{domain}", ms: "Live — buka https://{domain}", zh: "已上线 — 打开 https://{domain}", ta: "நேரடி — https://{domain} திறக்கவும்", ar: "مباشر — افتح https://{domain}" },
  "set.checkingDns": { en: "Checking DNS…", ms: "Menyemak DNS…", zh: "正在检查 DNS…", ta: "DNS சரிபார்க்கிறது…", ar: "جارٍ فحص DNS…" },
  "set.pollingHint": { en: "Auto-checking every 20s. No need to click again", ms: "Auto-semak setiap 20s — tak perlu klik lagi", zh: "每 20 秒自动检查 — 无需再次点击", ta: "ஒவ்வொரு 20 வினாடியும் தானாக சரிபார்க்கிறது — மீண்டும் கிளிக் தேவையில்லை", ar: "فحص تلقائي كل 20 ثانية — لا حاجة للنقر مرة أخرى" },
  "set.copy": { en: "Copy", ms: "Salin", zh: "复制", ta: "நகலெடு", ar: "نسخ" },
  "set.remove": { en: "Remove", ms: "Buang", zh: "移除", ta: "நீக்கு", ar: "إزالة" },
  // Issue #322: one-click auto-CNAME when Cloudflare is linked.
  "set.autoCnameHint": { en: "Cloudflare connected. We can create the CNAME for you.", ms: "Cloudflare bersambung — kami boleh cipta CNAME untuk anda.", zh: "Cloudflare 已连接 — 我们可以为您创建 CNAME。", ta: "Cloudflare இணைக்கப்பட்டது — உங்களுக்காக CNAME ஐ உருவாக்கலாம்.", ar: "تم الاتصال بـ Cloudflare — يمكننا إنشاء CNAME لك." },
  "set.autoCnameCta": { en: "Auto-create CNAME", ms: "Auto-cipta CNAME", zh: "自动创建 CNAME", ta: "CNAME தானாக உருவாக்கு", ar: "إنشاء CNAME تلقائياً" },
  "set.autoCnameBusy": { en: "Creating CNAME…", ms: "Mencipta CNAME…", zh: "正在创建 CNAME…", ta: "CNAME உருவாக்குகிறது…", ar: "جارٍ إنشاء CNAME…" },
  "set.autoCnameCreated": { en: "CNAME created automatically in your Cloudflare zone ✓", ms: "CNAME dicipta automatik dalam zon Cloudflare anda ✓", zh: "CNAME 已在您的 Cloudflare 区域自动创建 ✓", ta: "உங்கள் Cloudflare மண்டலத்தில் CNAME தானாக உருவாக்கப்பட்டது ✓", ar: "تم إنشاء CNAME تلقائياً في منطقة Cloudflare الخاصة بك ✓" },
  "set.autoCnameFailed": { en: "Could not auto-create the CNAME: {error}. You can set it manually.", ms: "Gagal auto-cipta: {error} — anda boleh set secara manual.", zh: "无法自动创建：{error} — 您可以手动设置。", ta: "தானாக உருவாக்க முடியவில்லை: {error} — கைமுறையாக அமைக்கலாம்.", ar: "تعذر الإنشاء التلقائي: {error} — يمكنك تعيينه يدوياً." },
  "set.autoCnameNotLinked": { en: "Cloudflare not linked. Set the CNAME manually.", ms: "Cloudflare tidak bersambung — set CNAME secara manual.", zh: "Cloudflare 未连接 — 请手动设置 CNAME。", ta: "Cloudflare இணைக்கப்படவில்லை — CNAME ஐ கைமுறையாக அமைக்கவும்.", ar: "Cloudflare غير متصل — عيّن CNAME يدوياً." },
  "set.autoCnameStatusCreated": { en: "CNAME auto-created via Cloudflare", ms: "CNAME auto-dicipta via Cloudflare", zh: "CNAME 已通过 Cloudflare 自动创建", ta: "Cloudflare வழியாக CNAME தானாக உருவாக்கப்பட்டது", ar: "تم إنشاء CNAME تلقائياً عبر Cloudflare" },
  "set.autoCnameStatusFailed": { en: "Auto-CNAME failed. Set it manually", ms: "Auto-CNAME gagal — set manual", zh: "自动 CNAME 失败 — 请手动设置", ta: "தானியங்கி CNAME தோல்வி — கைமுறையாக அமைக்கவும்", ar: "فشل CNAME التلقائي — عيّنه يدوياً" },
  "set.autoCnameStatusManual": { en: "Manual CNAME", ms: "CNAME manual", zh: "手动 CNAME", ta: "கைமுறை CNAME", ar: "CNAME يدوي" },
  // Issue #397: per-domain TLS config + status.
  "set.tlsModeLabel": { en: "TLS", ms: "TLS", zh: "TLS", ta: "TLS", ar: "TLS" },
  "set.tlsModeAutoLabel": { en: "Auto (Let's Encrypt)", ms: "Auto (Let's Encrypt)", zh: "自动（Let's Encrypt）", ta: "தானியங்கி (Let's Encrypt)", ar: "تلقائي (Let's Encrypt)" },
  "set.tlsModeCustomLabel": { en: "My own cert", ms: "Sijil sendiri", zh: "我自己的证书", ta: "எனது சொந்த சான்றிதழ்", ar: "شهادتي الخاصة" },
  "set.tlsModeAuto": { en: "Auto-TLS enabled. Let's Encrypt will issue a cert for this domain.", ms: "Auto-TLS diaktifkan — Let's Encrypt akan terbitkan sijil untuk domain ini.", zh: "已启用自动 TLS — Let's Encrypt 将为此域名签发证书。", ta: "தானியங்கி TLS இயக்கப்பட்டது — Let's Encrypt இந்த டொமைனுக்கு சான்றிதழ் வழங்கும்.", ar: "تم تفعيل TLS التلقائي — ستصدر Let's Encrypt شهادة لهذا النطاق." },
  "set.tlsModeCustom": { en: "Custom cert mode. Upload your certificate below.", ms: "Mod sijil sendiri — muat naik sijil anda di bawah.", zh: "自定义证书模式 — 请在下方上传您的证书。", ta: "தனிப்பயன் சான்றிதழ் முறை — கீழே உங்கள் சான்றிதழைப் பதிவேற்றவும்.", ar: "وضع الشهادة المخصصة — ارفع شهادتك أدناه." },
  "set.tlsModeFailed": { en: "Couldn't change TLS mode.", ms: "Gagal tukar mod TLS.", zh: "无法更改 TLS 模式。", ta: "TLS முறையை மாற்ற முடியவில்லை.", ar: "تعذر تغيير وضع TLS." },
  "set.tlsStatusActive": { en: "TLS active. Certificate issued and serving", ms: "TLS aktif — sijil diterbitkan & disajikan", zh: "TLS 已激活 — 证书已签发并生效", ta: "TLS செயலில் — சான்றிதழ் வழங்கப்பட்டு சேவை செய்கிறது", ar: "TLS نشط — تم إصدار الشهادة وتقديمها" },
  "set.tlsStatusPending": { en: "TLS pending. Certificate provisioning in progress", ms: "TLS menunggu — sijil sedang disediakan", zh: "TLS 待处理 — 证书正在签发中", ta: "TLS நிலுவையில் — சான்றிதழ் வழங்கல் நடைபெறுகிறது", ar: "TLS قيد الانتظار — جارٍ إصدار الشهادة" },
  "set.tlsStatusError": { en: "TLS error", ms: "Ralat TLS", zh: "TLS 错误", ta: "TLS பிழை", ar: "خطأ TLS" },
  "set.tlsStatusCustom": { en: "Custom certificate. Your own cert is being served", ms: "Sijil sendiri — sijil anda disajikan", zh: "自定义证书 — 正在提供您自己的证书", ta: "தனிப்பயன் சான்றிதழ் — உங்கள் சொந்த சான்றிதழ் வழங்கப்படுகிறது", ar: "شهادة مخصصة — يتم تقديم شهادتك الخاصة" },
  "set.tlsCertUpload": { en: "Upload cert", ms: "Muat naik sijil", zh: "上传证书", ta: "சான்றிதழைப் பதிவேற்று", ar: "رفع الشهادة" },
  "set.tlsCertClose": { en: "Close", ms: "Tutup", zh: "关闭", ta: "மூடு", ar: "إغلاق" },
  "set.tlsCertPlaceholder": { en: "Paste your certificate (PEM, full chain)", ms: "Tampal sijil anda (PEM, chain penuh)", zh: "粘贴您的证书（PEM，完整链）", ta: "உங்கள் சான்றிதழை ஒட்டவும் (PEM, முழு சங்கிலி)", ar: "الصق شهادتك (PEM، السلسلة الكاملة)" },
  "set.tlsKeyPlaceholder": { en: "Paste your private key (PEM)", ms: "Tampal kunci peribadi anda (PEM)", zh: "粘贴您的私钥（PEM）", ta: "உங்கள் தனிப்பட்ட விசையை ஒட்டவும் (PEM)", ar: "الصق مفتاحك الخاص (PEM)" },
  "set.tlsCertSave": { en: "Save cert", ms: "Simpan sijil", zh: "保存证书", ta: "சான்றிதழைச் சேமி", ar: "حفظ الشهادة" },
  "set.tlsCertSaving": { en: "Saving…", ms: "Menyimpan…", zh: "正在保存…", ta: "சேமிக்கிறது…", ar: "جارٍ الحفظ…" },
  "set.tlsCertSaved": { en: "Certificate saved. Your domain now serves your own cert ✓", ms: "Sijil disimpan — domain anda kini menyajikan sijil anda sendiri ✓", zh: "证书已保存 — 您的域名现在提供您自己的证书 ✓", ta: "சான்றிதழ் சேமிக்கப்பட்டது — உங்கள் டொமைன் இப்போது உங்கள் சொந்த சான்றிதழை வழங்குகிறது ✓", ar: "تم حفظ الشهادة — نطاقك يقدم الآن شهادتك الخاصة ✓" },
  "set.tlsCertFailed": { en: "Could not save the certificate. Check the PEM format and that the key matches.", ms: "Gagal simpan sijil — semak format PEM dan padanan kunci.", zh: "无法保存证书 — 请检查 PEM 格式和密钥匹配。", ta: "சான்றிதழைச் சேமிக்க முடியவில்லை — PEM வடிவம் மற்றும் விசை பொருத்தத்தைச் சரிபார்க்கவும்.", ar: "تعذر حفظ الشهادة — تحقق من تنسيق PEM ومطابقة المفتاح." },
  "set.tlsCertRequired": { en: "Both certificate and private key are required.", ms: "Sijil dan kunci peribadi kedua-duanya diperlukan.", zh: "证书和私钥都是必需的。", ta: "சான்றிதழ் மற்றும் தனிப்பட்ட விசை இரண்டும் தேவை.", ar: "كل من الشهادة والمفتاح الخاص مطلوبان." },
  "set.tlsEmailLabel": { en: "ACME contact email", ms: "Emel kenalan ACME", zh: "ACME 联系邮箱", ta: "ACME தொடர்பு மின்னஞ்சல்", ar: "البريد الإلكتروني للاتصال ACME" },
  "set.tlsEmailPlaceholder": { en: "owner@example.com (defaults to your account email)", ms: "owner@example.com (lalai ke emel akaun anda)", zh: "owner@example.com（默认为您的账户邮箱）", ta: "owner@example.com (இயல்பாக உங்கள் கணக்கு மின்னஞ்சல்)", ar: "owner@example.com (الافتراضي إلى بريد حسابك)" },
  "set.tlsEmailSave": { en: "Save email", ms: "Simpan emel", zh: "保存邮箱", ta: "மின்னஞ்சலைச் சேமி", ar: "حفظ البريد" },
  "set.tlsEmailSaving": { en: "Saving…", ms: "Menyimpan…", zh: "正在保存…", ta: "சேமிக்கிறது…", ar: "جارٍ الحفظ…" },
  "set.tlsEmailSaved": { en: "ACME contact email saved. This domain's cert will use it.", ms: "Emel kenalan ACME disimpan — sijil domain ini akan menggunakannya.", zh: "ACME 联系邮箱已保存 — 此域名的证书将使用它。", ta: "ACME தொடர்பு மின்னஞ்சல் சேமிக்கப்பட்டது — இந்த டொமைனின் சான்றிதழ் அதைப் பயன்படுத்தும்.", ar: "تم حفظ بريد ACME — ستستخدم شهادة هذا النطاق هذا البريد." },
  "set.tlsEmailFailed": { en: "Couldn't save the ACME contact email.", ms: "Gagal simpan emel kenalan ACME.", zh: "无法保存 ACME 联系邮箱。", ta: "ACME தொடர்பு மின்னஞ்சலைச் சேமிக்க முடியவில்லை.", ar: "تعذر حفظ بريد ACME." },
  "set.trackingTitle": { en: "Tracking scripts (global)", ms: "Skrip penjejakan (global)", zh: "跟踪脚本（全局）", ta: "கண்காணிப்பு ஸ்கிரிப்டுகள் (உலகளாவிய)", ar: "نصوص التتبع (عالمية)" },
  "set.trackingHelp": { en: "Injected into every public form head. Allowed:", ms: "Dimasukkan ke bahagian head setiap borang awam. Dibenarkan:", zh: "注入到每个公开表单的 head 中。允许：", ta: "ஒவ்வொரு பொது படிவத்தின் head-ல் செருகப்படும். அனுமதிக்கப்பட்டவை:", ar: "يُحقن في رأس كل نموذج عام. المسموح:" },
  "set.trackingSaved": { en: "Tracking saved ✓", ms: "Penjejakan disimpan ✓", zh: "跟踪已保存 ✓", ta: "கண்காணிப்பு சேமிக்கப்பட்டது ✓", ar: "تم حفظ التتبع ✓" },
  "set.saveTracking": { en: "Save tracking", ms: "Simpan penjejakan", zh: "保存跟踪", ta: "கண்காணிப்பைச் சேமி", ar: "حفظ التتبع" },
  "set.einvoiceTitle": { en: "e-Invoice (LHDN)", ms: "e-Invois (LHDN)", zh: "电子发票（LHDN）", ta: "மின் இன்வாய்ஸ் (LHDN)", ar: "الفاتورة الإلكترونية (LHDN)" },
  "set.einvoiceHelp": { en: "Buyer details collected on forms; TIN/NRIC auto-collected for B2B receipts. Receipt PDFs attach LHDN-ready fields.", ms: "Butiran pembeli dikutip pada borang; TIN/NRIC dikutip automatik untuk resit B2B. Resit PDF sertakan medan sedia LHDN.", zh: "表单收集买家信息；B2B 收据自动收集 TIN/NRIC。收据 PDF 附带 LHDN 所需字段。", ta: "படிவங்களில் வாங்குபவர் விவரங்கள் சேகரிக்கப்படும்; B2B ரசீதுகளுக்கு TIN/NRIC தானாக சேகரிக்கப்படும். ரசீது PDF களில் LHDN தயார் புலங்கள் இணைக்கப்படும்.", ar: "تُجمع تفاصيل المشتري في النماذج؛ ويُجمع TIN/NRIC تلقائياً للإيصالات B2B. ترفق ملفات الإيصالات PDF بحقول جاهزة لهيئة LHDN." },
  "set.companyTin": { en: "Company TIN", ms: "TIN syarikat", zh: "公司 TIN", ta: "நிறுவன TIN", ar: "الرقم الضريبي للشركة" },
  "set.msicCode": { en: "MSIC code", ms: "Kod MSIC", zh: "MSIC 代码", ta: "MSIC குறியீடு", ar: "رمز MSIC" },
  "set.saveEinvoice": { en: "Save e-Invoice", ms: "Simpan e-Invois", zh: "保存电子发票", ta: "மின் இன்வாய்ஸைச் சேமி", ar: "حفظ الفاتورة الإلكترونية" },
  "set.einvoiceSaved": { en: "e-Invoice details saved ✓", ms: "Butiran e-Invois disimpan ✓", zh: "电子发票信息已保存 ✓", ta: "மின் இன்வாய்ஸ் விவரங்கள் சேமிக்கப்பட்டன ✓", ar: "تم حفظ تفاصيل الفاتورة الإلكترونية ✓" },
  "set.webhooksTitle": { en: "Outbound webhooks", ms: "Webhook keluar", zh: "传出 Webhook", ta: "வெளிச்செல்லும் வெப்ஹூக்குகள்", ar: "خطافات الويب الصادرة" },
  "set.eventsLabel": { en: "Events:", ms: "Peristiwa:", zh: "事件：", ta: "நிகழ்வுகள்:", ar: "الأحداث:" },
  "set.hmacSigned": { en: "HMAC-SHA256 signed with your secret.", ms: "Ditandatangan HMAC-SHA256 dengan rahsia anda.", zh: "使用您的密钥进行 HMAC-SHA256 签名。", ta: "உங்கள் ரகசியத்துடன் HMAC-SHA256 கையொப்பமிடப்பட்டது.", ar: "موقّع بـ HMAC-SHA256 بمفتاحك السري." },
  "set.add": { en: "Add", ms: "Tambah", zh: "添加", ta: "சேர்க்க", ar: "إضافة" },
  "set.signingSecret": { en: "Signing secret", ms: "Rahsia penandatanganan", zh: "签名密钥", ta: "கையொப்ப ரகசியம்", ar: "سر التوقيع" },
  "set.clientId": { en: "client id:", ms: "id klien:", zh: "客户端 ID：", ta: "க்ளையன்ட் ஐடி:", ar: "معرف العميل:" },
  "set.revoke": { en: "Revoke", ms: "Tarik balik", zh: "撤销", ta: "ரத்து", ar: "إلغاء" },
  "set.serverUrl": { en: "Server URL", ms: "URL pelayan", zh: "服务器地址", ta: "சர்வர் URL", ar: "عنوان الخادم" },
  "set.cfTitle": { en: "Cloudflare (auto CNAME)", ms: "Cloudflare (CNAME automatik)", zh: "Cloudflare（自动 CNAME）", ta: "Cloudflare (தானியங்கி CNAME)", ar: "Cloudflare (CNAME تلقائي)" },
  "set.cfConnected": { en: "Cloudflare connected ✓", ms: "Cloudflare bersambung ✓", zh: "Cloudflare 已连接 ✓", ta: "Cloudflare இணைக்கப்பட்டது ✓", ar: "تم الاتصال بـ Cloudflare ✓" },
  "set.cfHelp": { en: "Log in with Cloudflare and JomForm auto-creates the CNAME record for each custom domain you add (routing via Traefik sync after Verify). No more manual DNS edits.", ms: "Log masuk dengan Cloudflare dan JomForm akan cipta rekod CNAME secara automatik untuk setiap domain tersuai yang anda tambah (routing via sync Traefik selepas Verify). Tak perlu CNAME manual lagi.", zh: "使用 Cloudflare 登录后，JomForm 会为您添加的每个自定义域名自动创建 CNAME 记录（验证后通过 Traefik 同步路由）。无需再手动添加 CNAME。", ta: "Cloudflare உடன் உள்நுழைந்தால், நீங்கள் சேர்க்கும் ஒவ்வொரு தனிப்பயன் டொமைனுக்கும் JomForm தானாக CNAME பதிவை உருவாக்கும் (Verify க்குப் பிறகு Traefik ஒத்திசைவு மூலம் ரூட்டிங்). கைமுறை CNAME தேவையில்லை.", ar: "بعد تسجيل الدخول عبر Cloudflare، ينشئ JomForm سجل CNAME تلقائياً لكل نطاق مخصص تضيفه (التوجيه عبر مزامنة Traefik بعد التحقق). لا حاجة لإضافة CNAME يدوياً." },
  "set.cfLogin": { en: "Log in with Cloudflare", ms: "Log masuk dengan Cloudflare", zh: "使用 Cloudflare 登录", ta: "Cloudflare உடன் உள்நுழை", ar: "تسجيل الدخول عبر Cloudflare" },
  "set.cfDisconnect": { en: "Disconnect", ms: "Putuskan", zh: "断开连接", ta: "துண்டி", ar: "قطع الاتصال" },
  "set.cfNotConfigured": { en: "Cloudflare auto-setup is not configured on this deployment. Add the CNAME record manually.", ms: "Auto-setup Cloudflare tidak dikonfigur pada pelayan ini — tambah rekod CNAME secara manual.", zh: "此部署未配置 Cloudflare 自动设置 — 请手动添加 CNAME 记录。", ta: "இந்த சர்வரில் Cloudflare தானியங்கி அமைப்பு கட்டமைக்கப்படவில்லை — CNAME பதிவை கைமுறையாக சேர்க்கவும்.", ar: "الإعداد التلقائي لـ Cloudflare غير مهيأ على هذا الخادم — أضف سجل CNAME يدوياً." },
  "set.cfManualToggle": { en: "Connect with API token", ms: "Sambung dengan API token", zh: "使用 API 令牌连接", ta: "API டோக்கன் மூலம் இணைக்கவும்", ar: "الاتصال عبر رمز API" },
  "set.cfManualHelp": { en: "No Cloudflare login? Paste an API token or global API key instead. Create one at:", ms: "Tak nak log masuk Cloudflare? Tampal API token atau global API key. Cipta di:", zh: "不想登录 Cloudflare？请粘贴 API 令牌或全局 API 密钥。在此创建：", ta: "Cloudflare உள்நுழைவு வேண்டாமா? API டோக்கன் அல்லது உலகளாவிய API விசையை ஒட்டவும். இங்கே உருவாக்கவும்:", ar: "لا تريد تسجيل الدخول عبر Cloudflare؟ الصق رمز API أو مفتاح API العام. أنشئه هنا:" },
  "set.cfApiTokensLink": { en: "Create an API token here", ms: "Cipta API token di sini", zh: "在此创建 API 令牌", ta: "API டோக்கனை இங்கே உருவாக்கவும்", ar: "أنشئ رمز API هنا" },
  "set.cfAccountName": { en: "Account name (optional)", ms: "Nama akaun (pilihan)", zh: "账户名称（可选）", ta: "கணக்கு பெயர் (விருப்பம்)", ar: "اسم الحساب (اختياري)" },
  "set.cfAccountEmail": { en: "Account email", ms: "Email akaun", zh: "账户邮箱", ta: "கணக்கு மின்னஞ்சல்", ar: "البريد الإلكتروني للحساب" },
  "set.cfApiToken": { en: "API token", ms: "API token", zh: "API 令牌", ta: "API டோக்கன்", ar: "رمز API" },
  "set.cfGlobalKey": { en: "Global API key", ms: "Global API key", zh: "全局 API 密钥", ta: "உலகளாவிய API விசை", ar: "مفتاح API العام" },
  "set.cfKindToken": { en: "API token", ms: "API token", zh: "API 令牌", ta: "API டோக்கன்", ar: "رمز API" },
  "set.cfKindGlobal": { en: "Global API key", ms: "Global API key", zh: "全局 API 密钥", ta: "உலகளாவிய API விசை", ar: "مفتاح API العام" },
  // issue #68: settings page tab labels + cloudflare callback banner
  "set.tab.chip": { en: "CHIP", ms: "CHIP", zh: "CHIP", ta: "CHIP", ar: "CHIP" },
  "set.tab.domains": { en: "Domains", ms: "Domain", zh: "域名", ta: "டொமைன்கள்", ar: "النطاقات" },
  "set.tab.tracking": { en: "Tracking", ms: "Penjejakan", zh: "跟踪", ta: "கண்காணிப்பு", ar: "التتبع" },
  "set.tab.einvoice": { en: "e-Invoice", ms: "e-Invois", zh: "电子发票", ta: "மின் இன்வாய்ஸ்", ar: "الفاتورة الإلكترونية" },
  // Card t_135c3c72: the buyer-borne processing fee (one MDR rate per CHIP
  // payment method). The acknowledgement sentence is deliberately blunt: it is
  // what the merchant agrees to before a fee can reach a buyer.
  "set.tab.fees": { en: "Payment fees", ms: "Caj bayaran", zh: "付款手续费", ta: "கட்டணக் கட்டணங்கள்", ar: "رسوم الدفع" },
  "set.feesTitle": { en: "Platform fee, paid by the buyer", ms: "Caj platform, ditanggung pembayar", zh: "平台费 — 由买家承担", ta: "தளக் கட்டணம் — வாங்குபவர் செலுத்துவார்", ar: "رسوم المنصة — يدفعها المشتري" },
  "set.feesApplies": { en: "Applies to new sales", ms: "Terpakai pada jualan baharu", zh: "适用于新销售", ta: "புதிய விற்பனைகளுக்குப் பொருந்தும்", ar: "يسري على المبيعات الجديدة" },
  "set.feesHelp": {
    en: "What JomForm adds to the buyer's total. It appears as its own line at checkout and on the receipt, so the item price never changes.",
    ms: "Apa yang JomForm tambah pada jumlah pembeli. Ia muncul sebagai baris tersendiri semasa pembayaran dan pada resit — harga item tidak berubah.",
    zh: "JomForm 加在买家总额上的费用。它在结账页和收据上以独立一行显示——商品价格不会改变。",
    ta: "JomForm வாங்குபவரின் மொத்தத்தில் சேர்ப்பது. இது பணம் செலுத்தும் பக்கத்திலும் ரசீதிலும் தனி வரியாகத் தோன்றும் — பொருளின் விலை மாறாது.",
    ar: "ما تضيفه JomForm إلى إجمالي المشتري. يظهر سطراً مستقلاً عند الدفع وعلى الإيصال — ولا يتغير سعر الصنف.",
  },
  "set.feesB2CDesc": { en: "Personal online banking", ms: "Perbankan dalam talian peribadi", zh: "个人网上银行", ta: "தனிநபர் இணைய வங்கி", ar: "الخدمات المصرفية الشخصية عبر الإنترنت" },
  "set.feesB2B1Desc": { en: "Corporate online banking", ms: "Perbankan dalam talian korporat", zh: "企业网上银行", ta: "நிறுவன இணைய வங்கி", ar: "الخدمات المصرفية للشركات عبر الإنترنت" },
  "set.feesCardDesc": { en: "Debit / credit", ms: "Debit / kredit", zh: "借记卡 / 信用卡", ta: "டெபிட் / கிரெடிட்", ar: "بطاقة خصم / ائتمان" },
  "set.feesQRDesc": { en: "Scan to pay", ms: "Imbas untuk bayar", zh: "扫码支付", ta: "பணம் செலுத்த ஸ்கேன் செய்க", ar: "امسح للدفع" },
  "set.feesEWalletDesc": { en: "GrabPay, Boost", ms: "GrabPay, Boost", zh: "GrabPay、Boost", ta: "GrabPay, Boost", ar: "GrabPay وBoost" },
  "set.feesHint": {
    en: "Leave a row blank for no fee. Charged per purchase in {cur}, stored as sen.",
    ms: "Biarkan baris kosong jika tiada caj. Dicaj setiap pembelian dalam {cur}, disimpan sebagai sen.",
    zh: "留空表示不收取费用。每次购买以 {cur} 收取，以分存储。",
    ta: "கட்டணம் இல்லை என்றால் வரிசையை காலியாக விடவும். ஒவ்வொரு வாங்குதலுக்கும் {cur}-இல் வசூலிக்கப்பட்டு, சென்னில் சேமிக்கப்படும்.",
    ar: "اترك الصف فارغاً لعدم وجود رسوم. تُحتسب لكل عملية شراء بـ {cur} وتُخزَّن بالسنت.",
  },
  "set.feesPreview": {
    en: "A buyer who pays {amount} with {method} is charged {total}.",
    ms: "Pembeli yang membayar {amount} dengan {method} dikenakan {total}.",
    zh: "使用 {method} 支付 {amount} 的买家将被收取 {total}。",
    ta: "{method} மூலம் {amount} செலுத்தும் வாங்குபவரிடம் {total} வசூலிக்கப்படும்.",
    ar: "المشتري الذي يدفع {amount} باستخدام {method} يُحتسب عليه {total}.",
  },
  "set.feesPreviewLines": {
    en: "{item} item + {fee} platform fee",
    ms: "{item} item + {fee} caj platform",
    zh: "{item} 商品 + {fee} 平台费",
    ta: "{item} பொருள் + {fee} தளக் கட்டணம்",
    ar: "سلعة بـ {item} + رسوم منصة {fee}",
  },
  "set.feesPreviewNoFee": {
    en: "{item} item, no platform fee",
    ms: "{item} item, tiada caj platform",
    zh: "{item} 商品，无平台费",
    ta: "{item} பொருள், தளக் கட்டணம் இல்லை",
    ar: "سلعة بـ {item}، دون رسوم منصة",
  },
  "set.feesReceiptLine": {
    en: "Receipt line: {line}",
    ms: "Baris resit: {line}",
    zh: "收据行：{line}",
    ta: "ரசீது வரி: {line}",
    ar: "سطر الإيصال: {line}",
  },
  "set.feesReceiptFeeLine": {
    en: "Platform fee {amount}",
    ms: "Caj platform {amount}",
    zh: "平台费 {amount}",
    ta: "தளக் கட்டணம் {amount}",
    ar: "رسوم المنصة {amount}",
  },
  "set.feesReceiptNone": { en: "(no fee line)", ms: "— (tiada baris caj)", zh: "—（无费用行）", ta: "— (கட்டண வரி இல்லை)", ar: "— (لا يوجد سطر رسوم)" },
  "set.feesNeverNamed": {
    en: "the payment method is never named on it",
    ms: "nama kaedah bayaran tidak pernah dipaparkan padanya",
    zh: "其上绝不显示付款方式名称",
    ta: "அதில் கட்டண முறையின் பெயர் ஒருபோதும் இடம்பெறாது",
    ar: "لا يُذكر اسم طريقة الدفع عليه أبداً",
  },
  "set.feesChipNote": {
    en: "CHIP's own fee is charged to you, the merchant, separately. It is never added to the buyer unless you set a platform fee that covers it.",
    ms: "Caj CHIP sendiri dikenakan kepada anda, pedagang, secara berasingan. Ia tidak pernah ditambah kepada pembeli melainkan anda menetapkan caj platform yang menampungnya.",
    zh: "CHIP 自身的费用另行向您（商家）收取，绝不会加到买家身上，除非您设置了足以覆盖它的平台费。",
    ta: "CHIP-இன் சொந்தக் கட்டணம் உங்களிடம் (வணிகர்) தனியாக வசூலிக்கப்படும். அதை ஈடுகட்டும் தளக் கட்டணத்தை நீங்கள் அமைத்தால் தவிர, அது வாங்குபவரிடம் சேர்க்கப்படாது.",
    ar: "رسوم CHIP الخاصة تُحتسب عليك أنت التاجر بشكل منفصل، ولا تُضاف إلى المشتري أبداً إلا إذا حددت رسوم منصة تغطيها.",
  },
  "set.feesLegacyTitle": {
    en: "Legacy rate, kept for one release",
    ms: "Kadar lama, dikekalkan untuk satu keluaran",
    zh: "旧版费率 — 保留一个版本",
    ta: "பழைய விகிதம் — ஒரு வெளியீட்டிற்கு மட்டும்",
    ar: "السعر القديم — محتفظ به لإصدار واحد",
  },
  "set.feesLegacyBody": {
    en: "This workspace still uses the old percentage model on {method}: {rate}. A buyer is still charged that amount until you move it.",
    ms: "Ruang kerja ini masih menggunakan model peratus lama pada {method}: {rate}. Pembeli masih dicaj jumlah itu sehingga anda memindahkannya.",
    zh: "此工作区在 {method} 上仍使用旧的百分比模式：{rate}。在您迁移之前，买家仍会被收取该金额。",
    ta: "இந்த பணியிடம் {method}-இல் பழைய சதவீத மாதிரியையே பயன்படுத்துகிறது: {rate}. நீங்கள் மாற்றும் வரை வாங்குபவரிடம் அந்தத் தொகை வசூலிக்கப்படும்.",
    ar: "لا تزال مساحة العمل هذه تستخدم نموذج النسبة المئوية القديم على {method}: {rate}. يظل المشتري يُحتسب عليه ذلك المبلغ حتى تنقله.",
  },
  "set.feesLegacyAmount": { en: "Platform amount ({cur})", ms: "Jumlah platform ({cur})", zh: "平台费金额（{cur}）", ta: "தளத் தொகை ({cur})", ar: "مبلغ المنصة ({cur})" },
  "set.feesLegacyMove": {
    en: "Move {method} to a platform fee",
    ms: "Pindahkan {method} ke caj platform",
    zh: "将 {method} 迁移为平台费",
    ta: "{method}-ஐ தளக் கட்டணமாக மாற்று",
    ar: "انقل {method} إلى رسوم منصة",
  },
  "set.feesLegacyNotice": {
    en: "Some payment methods still use the old percentage rate. They keep charging exactly as before until you move them.",
    ms: "Sesetengah kaedah bayaran masih menggunakan kadar peratus lama. Ia terus dicaj seperti sebelum ini sehingga anda memindahkannya.",
    zh: "部分付款方式仍使用旧的百分比费率。在您迁移之前，它们继续按原样收费。",
    ta: "சில கட்டண முறைகள் இன்னும் பழைய சதவீத விகிதத்தையே பயன்படுத்துகின்றன. நீங்கள் மாற்றும் வரை அவை முன்பு போலவே வசூலிக்கும்.",
    ar: "بعض طرق الدفع لا تزال تستخدم سعر النسبة المئوية القديم. وتستمر في التحصيل كما كانت تماماً حتى تنقلها.",
  },
  "set.feesLegacyNoticeGo": { en: "Review payment fees", ms: "Semak caj bayaran", zh: "查看付款手续费", ta: "கட்டணக் கட்டணங்களைப் பார்", ar: "راجع رسوم الدفع" },
  "set.feesMethod": { en: "Payment method", ms: "Kaedah bayaran", zh: "付款方式", ta: "கட்டண முறை", ar: "طريقة الدفع" },
  "set.feesFixed": { en: "Legacy fixed (sen)", ms: "Tetap lama (sen)", zh: "旧版固定额（分）", ta: "பழைய நிலையானது (சென்)", ar: "المبلغ الثابت القديم (سنت)" },
  "set.feesPercent": { en: "Legacy percent (basis points, 100 = 1%)", ms: "Peratus lama (basis point, 100 = 1%)", zh: "旧版百分比（基点，100 = 1%）", ta: "பழைய சதவீதம் (அடிப்படை புள்ளி, 100 = 1%)", ar: "النسبة القديمة (نقاط أساس، 100 = 1%)" },
  "set.feesAck": {
    en: "I understand this platform fee is charged to the buyer on top of the price and is separate from the fee CHIP charges me.",
    ms: "Saya faham caj platform ini dikenakan kepada pembayar di atas harga dan ia berasingan daripada caj CHIP ke atas saya.",
    zh: "我明白此平台费在价格之上向买家收取，并与 CHIP 向我收取的费用分开。",
    ta: "இந்தத் தளக் கட்டணம் விலையின் மேல் வாங்குபவரிடம் வசூலிக்கப்படுகிறது என்பதையும், அது CHIP என்னிடம் வசூலிக்கும் கட்டணத்திலிருந்து தனித்தது என்பதையும் நான் புரிந்துகொள்கிறேன்.",
    ar: "أفهم أن رسوم المنصة هذه تُحتسب على المشتري فوق السعر، وهي منفصلة عن الرسوم التي يحصّلها CHIP مني.",
  },
  "set.feesNeedAck": { en: "Tick the acknowledgement before enabling a fee.", ms: "Tandakan pengakuan sebelum mengaktifkan caj.", zh: "启用手续费前请勾选确认。", ta: "கட்டணத்தை இயக்கும் முன் ஒப்புதலைத் தெரிவுசெய்யவும்.", ar: "حدّد الإقرار قبل تفعيل الرسوم." },
  "set.feesSaved": { en: "Payment fees saved.", ms: "Caj bayaran disimpan.", zh: "手续费已保存。", ta: "கட்டணக் கட்டணங்கள் சேமிக்கப்பட்டன.", ar: "تم حفظ رسوم الدفع." },
  "set.feesSave": { en: "Save payment fees", ms: "Simpan caj bayaran", zh: "保存手续费", ta: "கட்டணங்களைச் சேமி", ar: "حفظ رسوم الدفع" },
  "set.tab.webhooks": { en: "Webhooks", ms: "Webhook", zh: "Webhook", ta: "வெப்ஹூக்குகள்", ar: "خطافات الويب" },
  // issue #129: MCP / connected agents get their own settings tab
  "set.tab.mcp": { en: "MCP", ms: "MCP", zh: "MCP", ta: "MCP", ar: "MCP" },
  // issue #107: per-workspace email template + behaviour settings
  "set.tab.emel": { en: "Email", ms: "Emel", zh: "电子邮件", ta: "மின்னஞ்சல்", ar: "البريد الإلكتروني" },
  // issue #185: per-workspace currency + BNM conversion
  "set.tab.currency": { en: "Currency", ms: "Mata wang", zh: "货币", ta: "நாணயம்", ar: "العملة" },
  "set.currencyTitle": { en: "Workspace currency", ms: "Mata wang ruang kerja", zh: "工作区货币", ta: "பணியிட நாணயம்", ar: "عملة مساحة العمل" },
  "set.currencyHelp": { en: "The currency new products and sales are denominated in (default MYR). The workspace's accounts inherit it for CHIP purchases. Changing it applies to new products and sales going forward. Historical sales keep their original currency and are never auto-converted, so refunds must be in the original currency.", ms: "Mata wang untuk produk dan jualan baharu (lalai MYR). Akaun space mewarisinya untuk pembelian CHIP. Perubahan terpakai pada produk/jualan baharu — jualan lama kekal mata wang asal dan tidak ditukar balik (bayaran balik mesti dalam mata wang asal).", zh: "新产品和销售使用的货币（默认 MYR）。空间的账户继承它用于 CHIP 购买。更改仅适用于新产品/销售 — 历史销售保留其原始货币，绝不自动转换（退款必须使用原始货币）。", ta: "புதிய தயாரிப்புகள் மற்றும் விற்பனைகள் குறிக்கப்படும் நாணயம் (இயல்புநிலை MYR). இடத்தின் கணக்குகள் CHIP கொள்முதல்களுக்கு அதைப் பெறுகின்றன. மாற்றம் புதிய தயாரிப்புகள்/விற்பனைகளுக்கு மட்டுமே பொருந்தும் — பழைய விற்பனைகள் அவற்றின் அசல் நாணயத்தை வைத்திருக்கும், ஒருபோதும் தானாக மாற்றப்படாது (பணத்திருப்பம் அசல் நாணயத்தில் இருக்க வேண்டும்).", ar: "العملة التي تُقوَّم بها المنتجات والمبيعات الجديدة (الافتراضي MYR). ترث حسابات المساحة ذلك لعمليات شراء CHIP. ينطبق التغيير على المنتجات/المبيعات الجديدة فقط — تحتفظ المبيعات التاريخية بعملتها الأصلية ولا تُحوَّل تلقائياً أبداً (يجب أن تكون المبالغ المستردة بالعملة الأصلية)." },
  "set.currencyLabel": { en: "Currency", ms: "Mata wang", zh: "货币", ta: "நாணயம்", ar: "العملة" },
  "set.currencySaved": { en: "Currency saved ✓", ms: "Mata wang disimpan ✓", zh: "货币已保存 ✓", ta: "நாணயம் சேமிக்கப்பட்டது ✓", ar: "تم حفظ العملة ✓" },
  "set.currencyRates": { en: "BNM exchange rates (daily)", ms: "Kadar tukaran BNM (harian)", zh: "BNM 汇率（每日）", ta: "BNM மாற்று விகிதங்கள் (தினசரி)", ar: "أسعار صرف BNM (يومياً)" },
  "set.currencyRatesHint": { en: "Rates from Bank Negara Malaysia, cached once daily. Reference data only: JomForm never converts a saved amount (sales keep their original currency). To convert a specific amount, use the convert_currency MCP tool.", ms: "Kadar dari Bank Negara Malaysia, dicache sekali sehari. Data rujukan — JomForm tidak sekali-kali menukar amaun yang telah disimpan (jualan kekal mata wang asal). Untuk menukar amaun tertentu, guna alat MCP convert_currency.", zh: "来自马来西亚国家银行的汇率，每日缓存一次。仅供参考 — JomForm 绝不转换已保存的金额（销售保留其原始货币）。如需转换特定金额，请使用 convert_currency MCP 工具。", ta: "Bank Negara Malaysia-இன் விகிதங்கள், ஒரு நாளைக்கு ஒருமுறை தற்காலிகமாக சேமிக்கப்படும். குறிப்பு தரவு மட்டுமே — JomForm சேமிக்கப்பட்ட தொகையை ஒருபோதும் மாற்றாது (விற்பனைகள் அசல் நாணயத்திலேயே இருக்கும்). ஒரு குறிப்பிட்ட தொகையை மாற்ற convert_currency MCP கருவியைப் பயன்படுத்துங்கள்.", ar: "أسعار من بنك نيجارا ماليزيا، مخزنة مؤقتاً مرة واحدة يومياً. بيانات مرجعية فقط — لا يحوّل JomForm أبداً مبلغاً محفوظاً (تحتفظ المبيعات بعملتها الأصلية). لتحويل مبلغ معيّن استخدم أداة MCP ‏convert_currency." },
  "set.currencyDocs": { en: "Currency & exchange rates docs", ms: "Dokumentasi mata wang & kadar tukaran", zh: "货币与汇率文档", ta: "நாணயம் & மாற்று விகித ஆவணங்கள்", ar: "وثائق العملة وأسعار الصرف" },
  "set.currencyCached": { en: "cached", ms: "dicache", zh: "已缓存", ta: "தற்காலிக சேமிப்பு", ar: "مخزنة مؤقتاً" },
  "set.currencyFresh": { en: "fresh", ms: "segar", zh: "最新", ta: "புதியது", ar: "حديثة" },
  "set.emelTitle": { en: "Receipt & notification email", ms: "Emel resit & notifikasi", zh: "收据与通知邮件", ta: "ரசீது & அறிவிப்பு மின்னஞ்சல்", ar: "بريد الإيصال والإشعار" },
  "set.emelHelp": { en: "Customise the receipt email JomForm sends to buyers on payment, and optionally notify you of each new sale. Defaults match the standard receipt, so leave them unchanged to keep current behaviour.", ms: "Sesuaikan emel resit yang JomForm hantar kepada pembeli selepas bayaran, dan pilihan untuk memaklumkan anda setiap jualan baharu. Lalai sepadan dengan resit standard — biarkan untuk kekalkan tingkah laku semasa.", zh: "自定义 JomForm 在付款时发送给买家的收据邮件，并可选择在每笔新销售时通知您。默认值与标准收据一致 — 保持不变即可维持当前行为。", ta: "கட்டணம் செலுத்தியதும் JomForm வாங்குபவருக்கு அனுப்பும் ரசீது மின்னஞ்சலைத் தனிப்பயனாக்குங்கள், மேலும் ஒவ்வொரு புதிய விற்பனைக்கும் உங்களுக்கு அறிவிக்கலாம். இயல்புநிலைகள் நிலையான ரசீதுடன் பொருந்தும் — மாற்றாமல் விட்டால் தற்போதைய நடத்தை நீடிக்கும்.", ar: "خصص بريد الإيصال الذي يرسله JomForm للمشترين عند الدفع، واختيارياً إشعارك بكل عملية بيع جديدة. تتطابق الافتراضيات مع الإيصال القياسي — اتركها دون تغيير للحفاظ على السلوك الحالي." },
  "set.emelSubject": { en: "Subject", ms: "Subjek", zh: "主题", ta: "பொருள்", ar: "الموضوع" },
  // One key for BOTH receipt-subject fields (Settings → Email and Form settings →
  // Email): the empty-field suggestion is the same sentence, and two copies drift.
  "set.emelSubjectPh": { en: "Payment receipt {{ref_code}}", ms: "Resit pembayaran — {{ref_code}}", zh: "付款收据 — {{ref_code}}", ta: "பணம் செலுத்திய ரசீது — {{ref_code}}", ar: "إيصال الدفع — {{ref_code}}" },
  "set.emelBody": { en: "Body (template)", ms: "Badan (templat)", zh: "正文（模板）", ta: "உடல் (வார்ப்புரு)", ar: "المحتوى (قالب)" },
  "set.emelPlaceholders": { en: "Placeholders:", ms: "Placeholder:", zh: "占位符：", ta: "பிளேஸ்ஹோல்டர்கள்:", ar: "الرموز:" },
  "set.emelLanguageRule": { en: "Subject and body are judged separately: editing one keeps the other on your form's language.", ms: "Subjek dan badan dinilai berasingan: mengedit salah satu mengekalkan yang lain pada bahasa borang anda.", zh: "主题和正文分开判断：编辑其中一个，另一个仍跟随您表单的语言。", ta: "தலைப்பும் உள்ளடக்கமும் தனித்தனியாக மதிப்பிடப்படும்: ஒன்றைத் திருத்தினால் மற்றது உங்கள் படிவத்தின் மொழியில் இருக்கும்.", ar: "يُقيَّم الموضوع والنص بشكل منفصل: تحرير أحدهما يُبقي الآخر على لغة نموذجك." },
  "set.emelReceiptEnabled": { en: "Send receipt to buyer", ms: "Hantar resit kepada pembeli", zh: "向买家发送收据", ta: "வாங்குபவருக்கு ரசீது அனுப்பு", ar: "إرسال الإيصال إلى المشتري" },
  "set.emelNotifyMerchant": { en: "Also notify me of each new sale", ms: "Juga maklumkan saya setiap jualan baharu", zh: "同时通知我每笔新销售", ta: "ஒவ்வொரு புதிய விற்பனைக்கும் எனக்கும் அறிவி", ar: "أبلغني أيضاً بكل عملية بيع جديدة" },
  "set.emelMerchantEmail": { en: "Notification email (blank = account email)", ms: "Emel notifikasi (kosong = emel akaun)", zh: "通知邮箱（留空 = 账户邮箱）", ta: "அறிவிப்பு மின்னஞ்சல் (காலியாக = கணக்கு மின்னஞ்சல்)", ar: "بريد الإشعار (فارغ = بريد الحساب)" },
  "set.emelHtml": { en: "HTML body (auto-escaped, no scripts)", ms: "Badan HTML (auto-escape, tiada skrip)", zh: "HTML 正文（自动转义，无脚本）", ta: "HTML உடல் (தானியங்கி எஸ்கேப், ஸ்கிரிப்ட் இல்லை)", ar: "محتوى HTML (تلقائي الترميز، بلا نصوص برمجية)" },
  "set.emelSave": { en: "Save email settings", ms: "Simpan tetapan emel", zh: "保存邮件设置", ta: "மின்னஞ்சல் அமைப்புகளைச் சேமி", ar: "حفظ إعدادات البريد" },
  "set.emelSaved": { en: "Email settings saved ✓", ms: "Tetapan emel disimpan ✓", zh: "邮件设置已保存 ✓", ta: "மின்னஞ்சல் அமைப்புகள் சேமிக்கப்பட்டன ✓", ar: "تم حفظ إعدادات البريد ✓" },
  "set.emelPreview": { en: "Preview", ms: "Pratonton", zh: "预览", ta: "முன்னோட்டம்", ar: "معاينة" },
  "set.emelPreviewHint": { en: "Preview uses sample data.", ms: "Pratonton guna data contoh.", zh: "预览使用示例数据。", ta: "முன்னோட்டம் மாதிரி தரவைப் பயன்படுத்துகிறது.", ar: "تستخدم المعاينة بيانات نموذجية." },
  // issue #183: per-workspace public-form footer (Storefront tab)
  "set.tab.storefront": { en: "Storefront", ms: "Kedai", zh: "店铺", ta: "கடை", ar: "المتجر" },
  "set.sfTitle": { en: "Public form footer", ms: "Footer borang awam", zh: "公开表单页脚", ta: "பொது படிவ கால்பகுதி", ar: "تذييل النموذج العام" },
  "set.sfHelp": { en: "Optional footer shown on your public forms. Only the fields you fill in are displayed. Leave everything blank for no footer.", ms: "Footer pilihan yang dipaparkan pada borang awam anda. Hanya medan yang anda isi dipaparkan — biarkan kosong untuk tiada footer.", zh: "显示在公开表单上的可选页脚。仅显示您填写的字段 — 全部留空则不显示页脚。", ta: "உங்கள் பொது படிவங்களில் காட்டப்படும் விருப்ப கால்பகுதி. நீங்கள் நிரப்பும் புலங்கள் மட்டுமே காட்டப்படும் — அனைத்தையும் காலியாக விட்டால் கால்பகுதி இல்லை.", ar: "تذييل اختياري يظهر في نماذجك العامة. تُعرض الحقول التي تملؤها فقط — اترك كل شيء فارغاً لعدم وجود تذييل." },
  "set.sfBusinessName": { en: "Business name", ms: "Nama perniagaan", zh: "商家名称", ta: "வணிகப் பெயர்", ar: "اسم النشاط التجاري" },
  "set.sfCompanyReg": { en: "Business registration number", ms: "Nombor pendaftaran perniagaan", zh: "商业注册号", ta: "வணிகப் பதிவு எண்", ar: "رقم تسجيل النشاط التجاري" },
  "set.sfContactPhone": { en: "Contact phone", ms: "Nombor telefon", zh: "联系电话", ta: "தொடர்பு தொலைபேசி", ar: "هاتف التواصل" },
  "set.sfContactAddress": { en: "Address", ms: "Alamat", zh: "地址", ta: "முகவரி", ar: "العنوان" },
  "set.sfPrivacyUrl": { en: "Privacy policy URL (https://)", ms: "URL polisi privasi (https://)", zh: "隐私政策 URL（https://）", ta: "தனியுரிமைக் கொள்கை URL (https://)", ar: "رابط سياسة الخصوصية (https://)" },
  "set.sfTermsUrl": { en: "Terms of service URL (https://)", ms: "URL terma & syarat (https://)", zh: "服务条款 URL（https://）", ta: "சேவை விதிமுறைகள் URL (https://)", ar: "رابط شروط الخدمة (https://)" },
  "set.sfRefundUrl": { en: "Refund policy URL (https://)", ms: "URL polisi bayaran balik (https://)", zh: "退款政策 URL（https://）", ta: "பணத்திருப்புக் கொள்கை URL (https://)", ar: "رابط سياسة الاسترداد (https://)" },
  // Card t_e8b4f7da: the two reasons the API's footer rule can give for NOT
  // publishing a saved legal URL. Shown under the field itself, because the save
  // still succeeds and still says "Footer saved ✓" — without this the merchant
  // believes the link is live while the footer silently emits nothing.
  "set.sfLegalLinkNotServed": { en: "Not shown on your public forms: JomForm serves a hosted legal page only at {served} (no trailing slash, no extra path). Type that exact URL, or link to a page on your own website.", ms: "Tidak dipaparkan pada borang awam anda: JomForm hanya menyediakan halaman undang-undang yang dihoskan di {served} — tanpa garis miring di hujung dan tanpa laluan tambahan. Taip URL tepat itu, atau pautkan ke halaman di laman web anda sendiri.", zh: "不会显示在您的公开表单上：JomForm 仅在此地址提供托管的法务页面 {served} — 结尾没有斜杠，也不能有多余路径。请填写该确切 URL，或链接到您自己网站的页面。", ta: "உங்கள் பொது படிவங்களில் காட்டப்படாது: JomForm ஹோஸ்ட் செய்யும் சட்டப் பக்கத்தை {served} இல் மட்டுமே வழங்குகிறது — இறுதியில் slash இல்லாமல், கூடுதல் பாதை இல்லாமல். அதே URL-ஐத் தட்டச்சு செய்யவும், அல்லது உங்கள் சொந்த இணையதளப் பக்கத்திற்கு இணைக்கவும்.", ar: "لن يظهر في نماذجك العامة: لا يقدّم JomForm الصفحة القانونية المستضافة إلا على {served} — دون شرطة مائلة في النهاية ودون مسار إضافي. اكتب هذا العنوان بالضبط، أو اربط بصفحة على موقعك الخاص." },
  "set.sfLegalLinkNoContent": { en: "Not shown on your public forms: the hosted page {served} has no saved content yet. Add its text in the Legal tab and save the footer again, or link to a page on your own website.", ms: "Tidak dipaparkan pada borang awam anda: halaman yang dihoskan {served} belum mempunyai kandungan. Tambah teksnya di tab Halaman undang-undang dan simpan footer semula — atau pautkan ke halaman di laman web anda sendiri.", zh: "不会显示在您的公开表单上：托管页面 {served} 尚无已保存的内容。请在“法律页面”标签页添加内容并重新保存页脚 — 或链接到您自己网站的页面。", ta: "உங்கள் பொது படிவங்களில் காட்டப்படாது: ஹோஸ்ட் செய்யப்பட்ட பக்கம் {served} இல் இன்னும் உள்ளடக்கம் சேமிக்கப்படவில்லை. 'சட்டப் பக்கங்கள்' தாவலில் உரையைச் சேர்த்து மீண்டும் சேமிக்கவும் — அல்லது உங்கள் சொந்த இணையதளப் பக்கத்திற்கு இணைக்கவும்.", ar: "لن يظهر في نماذجك العامة: الصفحة المستضافة {served} لا تحتوي على محتوى محفوظ بعد. أضف نصها في تبويب الصفحات القانونية ثم احفظ التذييل مرة أخرى — أو اربط بصفحة على موقعك الخاص." },
  // The inline "?" beside those three fields (docs parity, step 4): how the
  // footer decides which links to publish.
  "set.sfLegalLinkDocs": { en: "Which legal links appear on my forms?", ms: "Pautan undang-undang mana yang muncul pada borang saya?", zh: "哪些法务链接会显示在我的表单上？", ta: "எந்தச் சட்ட இணைப்புகள் என் படிவங்களில் தோன்றும்?", ar: "أي روابط قانونية تظهر في نماذجي؟" },
  "set.sfFooterText": { en: "Footer text (optional)", ms: "Teks footer (pilihan)", zh: "页脚文本（可选）", ta: "கால்பகுதி உரை (விருப்பம்)", ar: "نص التذييل (اختياري)" },
  "set.sfIndexingTitle": { en: "Allow search engines & AI to index this workspace", ms: "Benarkan enjin carian & AI mengindeks ruang kerja ini", zh: "允许搜索引擎和 AI 索引此工作区", ta: "இந்த பணியிடத்தை தேடுபொறிகள் & AI குறியீடு செய்ய அனுமதி", ar: "السماح لمحركات البحث والذكاء الاصطناعي بفهرسة مساحة العمل هذه" },
  "set.sfIndexingHelp": { en: "When on, your public forms on a custom domain appear in search results and AI. Forms on jomform.com subdomains stay hidden for privacy.", ms: "Apabila dihidupkan, borang awam anda pada domain tersuai muncul dalam hasil carian dan AI. Borang pada subdomain jomform.com kekal tersembunyi demi privasi.", zh: "开启后，自定义域名上的公开表单会出现在搜索结果和 AI 中。jomform.com 子域名上的表单出于隐私保持隐藏。", ta: "இயக்கத்தில் இருக்கும்போது, தனிப்பயன் டொமைனில் உள்ள உங்கள் பொது படிவங்கள் தேடல் முடிவுகளிலும் AI-யிலும் தோன்றும். jomform.com துணை டொமைன்களில் உள்ள படிவங்கள் தனியுரிமைக்காக மறைக்கப்படும்.", ar: "عند التفعيل، تظهر نماذجك العامة على نطاق مخصص في نتائج البحث والذكاء الاصطناعي. تبقى النماذج على نطاقات فرعية jomform.com مخفية للخصوصية." },
  // Card t_48a93084: the same help on a SELF-HOSTED install, which has no
  // jomform.com subdomains to hide — one origin serves every workspace.
  "set.sfIndexingHelpSelf": { en: "When on, your public forms on a custom domain appear in search results and AI. Forms on this install's own address stay hidden for privacy.", ms: "Apabila dihidupkan, borang awam anda pada domain tersuai muncul dalam hasil carian dan AI. Borang pada alamat pemasangan ini kekal tersembunyi demi privasi.", zh: "开启后，自定义域名上的公开表单会出现在搜索结果和 AI 中。此安装自身地址上的表单出于隐私保持隐藏。", ta: "இயக்கத்தில் இருக்கும்போது, தனிப்பயன் டொமைனில் உள்ள உங்கள் பொது படிவங்கள் தேடல் முடிவுகளிலும் AI-யிலும் தோன்றும். இந்த நிறுவலின் சொந்த முகவரியில் உள்ள படிவங்கள் தனியுரிமைக்காக மறைக்கப்படும்.", ar: "عند التفعيل، تظهر نماذجك العامة على نطاق مخصص في نتائج البحث والذكاء الاصطناعي. تبقى النماذج على عنوان هذا التنصيب مخفية للخصوصية." },
  "set.sfSave": { en: "Save footer", ms: "Simpan footer", zh: "保存页脚", ta: "கால்பகுதியைச் சேமி", ar: "حفظ التذييل" },
  // Language placement: the workspace's default language for its PUBLIC forms —
  // the buyer-facing fallback when a form has no per-form language. Distinct
  // from the per-profile dashboard language on /profile.
  "set.sfDefaultLang": { en: "Default form language", ms: "Bahasa borang lalai", zh: "默认表单语言", ta: "இயல்புநிலை படிவ மொழி", ar: "اللغة الافتراضية للنموذج" },
  "set.sfDefaultLangAuto": { en: "English (default)", ms: "Bahasa Inggeris (lalai)", zh: "英语（默认）", ta: "ஆங்கிலம் (இயல்புநிலை)", ar: "الإنجليزية (افتراضي)" },
  "set.sfDefaultLangHelp": { en: "The language your customers see on your public forms, unless a form sets its own. Your dashboard language is separate; change that on your Profile page.", ms: "Bahasa yang pelanggan anda lihat pada borang awam anda, kecuali borang itu menetapkan bahasanya sendiri. Bahasa dashboard anda berasingan — tukar di halaman Profil anda.", zh: "客户在您的公开表单上看到的语言，除非该表单自行设置了语言。您的仪表板语言是分开的 — 请在个人资料页面更改。", ta: "ஒரு படிவம் அதன் சொந்த மொழியை அமைக்காத வரை, உங்கள் பொது படிவங்களில் உங்கள் வாடிக்கையாளர்கள் காணும் மொழி. உங்கள் டாஷ்போர்டு மொழி தனித்துவமானது — அதை உங்கள் சுயவிவரப் பக்கத்தில் மாற்றவும்.", ar: "اللغة التي يراها عملاؤك على نماذجك العامة، ما لم يحدد النموذج لغته الخاصة. لغة لوحة التحكم منفصلة — غيّرها من صفحة ملفك الشخصي." },
  "set.sfSaved": { en: "Footer saved ✓", ms: "Footer disimpan ✓", zh: "页脚已保存 ✓", ta: "கால்பகுதி சேமிக்கப்பட்டது ✓", ar: "تم حفظ التذييل ✓" },
  // The short form shown ON the button itself for 2s after a successful save.
  // Distinct from `set.sfSaved` (which the page-top banner uses and which names
  // the surface): a 2s confirmation on a button has room for one word, and
  // "Footer saved ✓" would make the button jump wider as it switches.
  "set.sfSavedShort": { en: "Saved ✓", ms: "Disimpan ✓", zh: "已保存 ✓", ta: "சேமிக்கப்பட்டது ✓", ar: "تم الحفظ ✓" },
  // issue #447: per-workspace realtime notification sound toggle
  "set.sfSoundTitle": { en: "Play a beep when a new order arrives", ms: "Bunyikan beep bila pesanan baharu tiba", zh: "新订单到达时播放提示音", ta: "புதிய ஆர்டர் வரும்போது பீப் ஒலி", ar: "تشغيل صوت تنبيه عند وصول طلب جديد" },
  "set.sfSoundHelp": { en: "When on, an open dashboard (for example a kitchen or order screen) plays a short beep and updates live when a new sale or response arrives. No manual refresh. Off by default.", ms: "Apabila dihidupkan, dashboard yang terbuka (cth skrin dapur) berbunyi beep pendek dan dikemas kini secara langsung bila jualan/respon baharu tiba — tanpa muat semula. Lalai dimatikan.", zh: "开启后，打开的仪表板（如厨房/订单屏幕）会在新销售或响应到达时播放短提示音并实时更新 — 无需手动刷新。默认关闭。", ta: "இயக்கத்தில் இருக்கும்போது, திறந்த டாஷ்போர்டு (எ.கா. சமையலறை/ஆர்டர் திரை) புதிய விற்பனை அல்லது பதில் வரும்போது குறுகிய பீப் ஒலித்து நேரலையில் புதுப்பிக்கும் — கைமுறை புதுப்பிப்பு தேவையில்லை. இயல்பாக முடக்கப்பட்டது.", ar: "عند التفعيل، تصدر لوحة التحكم المفتوحة (مثل شاشة المطبخ/الطلبات) صوت تنبيه قصيراً وتتحدث مباشرة عند وصول بيع أو رد جديد — دون تحديث يدوي. معطّل افتراضياً." },
  // this change: per-workspace AI spam-judge toggle
  "set.sfSpamTitle": { en: "AI spam detection", ms: "Pengesanan spam AI", zh: "AI 垃圾内容检测", ta: "AI ஸ்பேம் கண்டறிதல்", ar: "كشف الرسائل المزعجة بالذكاء الاصطناعي" },
  "set.sfSpamHelp": { en: "When on (default), every new submission or sale is sent to an AI judge in the background and may be flagged as spam. When off, no entry is ever judged: nothing is flagged, so the Spam page stays empty and no flagged entry can be auto-deleted after 30 days. Turn it off if real entries are being flagged.", ms: "Apabila dihidupkan (lalai), setiap penghantaran atau jualan baharu dihantar kepada hakim AI di latar belakang dan mungkin ditanda sebagai spam. Apabila dimatikan, tiada entri dihakimi: tiada apa ditanda, jadi halaman Spam kekal kosong dan tiada entri bertanda dipadam automatik selepas 30 hari. Matikan jika entri sebenar ditanda spam.", zh: "开启时（默认），每份新提交或销售都会在后台交给 AI 判定，可能被标记为垃圾内容。关闭后不会有任何条目被判定：不会被标记，垃圾内容页面保持为空，也不会有被标记的条目在 30 天后被自动删除。若有真实条目被误标，请关闭此项。", ta: "இயக்கத்தில் இருக்கும்போது (இயல்புநிலை), ஒவ்வொரு புதிய சமர்ப்பிப்பும் விற்பனையும் பின்னணியில் AI நடுவரிடம் அனுப்பப்படும், ஸ்பேம் எனக் குறிக்கப்படலாம். அணைத்தால் எந்தப் பதிவும் தீர்ப்பிடப்படாது: எதுவும் குறிக்கப்படாது, எனவே ஸ்பேம் பக்கம் காலியாக இருக்கும், குறிக்கப்பட்ட பதிவு 30 நாட்களுக்குப் பிறகு தானாக நீக்கப்படாது. உண்மையான பதிவுகள் குறிக்கப்பட்டால் இதை அணைக்கவும்.", ar: "عند التفعيل (افتراضي)، يُرسل كل إرسال أو بيع جديد إلى مُحكِّم ذكاء اصطناعي في الخلفية وقد يُعلَّم كرسالة مزعجة. عند الإيقاف، لا يُحكَّم أي إدخال: لا شيء يُعلَّم، وتبقى صفحة الرسائل المزعجة فارغة، ولن يُحذف أي إدخال معلَّم تلقائياً بعد 30 يوماً. أوقفه إذا كانت إدخالات حقيقية تُعلَّم." },
  "set.sfSpamDocs": { en: "How AI spam detection works (docs)", ms: "Cara pengesanan spam AI berfungsi (dokumen)", zh: "AI 垃圾内容检测如何运作（文档）", ta: "AI ஸ்பேம் கண்டறிதல் எவ்வாறு இயங்குகிறது (ஆவணங்கள்)", ar: "كيف يعمل كشف الرسائل المزعجة بالذكاء الاصطناعي (المستندات)" },
  // issue #360: per-workspace customizable landing page
  "set.lpTitle": { en: "Landing page", ms: "Landing page", zh: "落地页", ta: "லேண்டிங் பக்கம்", ar: "صفحة الهبوط" },
  "set.lpHelp": { en: "Design the storefront home page shown at {host} and on your custom domain. Leave the toggle off to keep the automatic form listing.", ms: "Reka halaman utama storefront yang dipaparkan di {host} dan pada domain tersuai anda. Matikan suis untuk mengekalkan senarai borang automatik.", zh: "设计 {host} 及自定义域名上显示的店铺首页。关闭开关则保持自动表单列表。", ta: "{host} மற்றும் உங்கள் தனிப்பயன் டொமைனில் காட்டப்படும் கடையின் முகப்புப்பக்கத்தை வடிவமைக்கவும். தானியங்கி படிவப் பட்டியலை வைக்க தேர்வை அணைக்கவும்.", ar: "صمم الصفحة الرئيسية للمتجر المعروضة على {host} و نطاقك المخصص. أبقِ المفتاح مغلقاً للحفاظ على قائمة النماذج التلقائية." },
  "set.lpEnabledTitle": { en: "Show a custom landing page", ms: "Papar landing page tersuai", zh: "显示自定义落地页", ta: "தனிப்பயன் லேண்டிங் பக்கத்தைக் காட்டு", ar: "عرض صفحة هبوط مخصصة" },
  "set.lpEnabledHelp": { en: "When off, the storefront shows the auto-generated list of your published forms.", ms: "Apabila dimatikan, storefront menunjukkan senarai borang diterbitkan anda secara automatik.", zh: "关闭时，店铺显示自动生成的已发布表单列表。", ta: "அணைக்கப்படும்போது, கடை உங்கள் வெளியிடப்பட்ட படிவங்களின் தானியங்கி பட்டியலைக் காட்டும்.", ar: "عند إيقاف التشغيل، يعرض المتجر القائمة التلقائية لنماذجك المنشورة." },
  "set.lpHero": { en: "Hero heading", ms: "Tajuk utama (hero)", zh: "主标题（Hero）", ta: "முக்கிய தலைப்பு (Hero)", ar: "العنوان الرئيسي (Hero)" },
  // Placeholders for the landing-page fields: text a merchant READS in the empty
  // field, so it follows the dashboard language (t_541a31d5). The `en` arm is the
  // suggestion the Malay arm used to ship hardcoded in the component.
  "set.lpHeroPh": { en: "Welcome to our shop", ms: "Selamat datang ke kedai kami", zh: "欢迎光临我们的商店", ta: "எங்கள் கடைக்கு வருக", ar: "مرحباً بك في متجرنا" },
  "set.lpSubtitle": { en: "Subtitle", ms: "Sub-tajuk", zh: "副标题", ta: "துணைத் தலைப்பு", ar: "العنوان الفرعي" },
  "set.lpSubtitlePh": { en: "Special items for you", ms: "Barang-barang istimewa untuk anda", zh: "为您精选的商品", ta: "உங்களுக்கான சிறப்புப் பொருட்கள்", ar: "منتجات مميزة لك" },
  "set.lpBody": { en: "Body text", ms: "Teks badan", zh: "正文", ta: "உடல் உரை", ar: "نص أساسي" },
  "set.lpImageUrl": { en: "Hero / banner image URL (https://)", ms: "URL imej hero/banner (https://)", zh: "主图 / 横幅图片 URL（https://）", ta: "Hero/பேனர் பட URL (https://)", ar: "رابط صورة الرئيسية/اللافتة (https://)" },
  // Issue #501: shown when the backend rejects a non-https image/URL value
  // (form logo, landing image/logo, product image, builder image block). The
  // value is rejected, not stored, so the buyer's page never gets the
  // html/template #ZgotmplZ sentinel in place of a real image.
  "url.notHttps": {
    en: "An image URL must start with https://. Other schemes (data:, http:, javascript:) are rejected so the image never breaks on the buyer's page.",
    ms: "URL imej mesti bermula dengan https:// — skema lain (data:, http:, javascript:, …) ditolak supaya imej tidak rosak pada halaman pembeli.",
    zh: "图片/链接必须以 https:// 开头 — 其他协议（data:、http:、javascript: 等）会被拒绝，以免买家页面上的图片损坏。",
    ta: "படம்/URL https:// என்று தொடங்க வேண்டும் — மற்ற திட்டங்கள் (data:, http:, javascript:, …) நிராகரிக்கப்படும், வாங்குபவர் பக்கத்தில் படம் உடையாது.",
    ar: "يجب أن يبدأ رابط الصورة بـ https:// — يتم رفض المخططات الأخرى (data:، http:، javascript:، …) حتى لا تنكسر الصورة في صفحة المشتري.",
  },
  "set.lpLogoUrl": { en: "Logo URL (https://)", ms: "URL logo (https://)", zh: "Logo URL（https://）", ta: "Logo URL (https://)", ar: "رابط الشعار (https://)" },
  // Issue #6177: the API answers every failure with a stable machine `code`
  // (api/internal/httpapi/errcode.go) that the dashboard translates here.
  // These are the codes the SERVER names. A code with no entry below is not
  // translated on purpose: the calling page renders its own message, so an
  // unknown code can never fall back to the server's prose.
  "apiErr.workspaceNotFound": {
    en: "That workspace was not found, or it is not one you have access to.",
    ms: "Workspace itu tidak dijumpai, atau ia bukan milik anda.",
    zh: "未找到该工作区，或您无权访问它。",
    ta: "அந்த பணியிடம் கிடைக்கவில்லை, அல்லது அது உங்களுக்குச் சொந்தமானது அல்ல.",
    ar: "لم يتم العثور على مساحة العمل هذه، أو أنها ليست من مساحاتك.",
  },
  "apiErr.workspaceSlugTaken": { en: "That slug is already taken.", ms: "Slug itu sudah digunakan.", zh: "该 Slug 已被占用。", ta: "அந்த slug ஏற்கனவே பயன்பாட்டில் உள்ளது.", ar: "هذا الـ slug مستخدم بالفعل." },
  "apiErr.workspaceSlugInvalid": { en: "Slug must be 3-40 chars, a-z, 0-9, dash.", ms: "Slug mesti 3-40 aksara, a-z, 0-9, dash.", zh: "Slug 必须为 3-40 个字符，仅限 a-z、0-9、连字符。", ta: "Slug 3-40 எழுத்துகள், a-z, 0-9, dash ஆக இருக்க வேண்டும்.", ar: "يجب أن يكون الـ slug من 3-40 حرفاً، a-z، 0-9، شرطة." },
  "apiErr.workspaceNameTooLong": { en: "Workspace name must be 120 characters or fewer.", ms: "Nama workspace maksimum 120 aksara.", zh: "工作区名称最多 120 个字符。", ta: "பணியிடப் பெயர் 120 எழுத்துகளுக்கு மிகாமல் இருக்க வேண்டும்.", ar: "يجب ألا يزيد اسم مساحة العمل عن 120 حرفاً." },
  "apiErr.workspaceRequired": { en: "Create a workspace before creating a form. A form needs one to be published at a working address.", ms: "Buat workspace dahulu sebelum mencipta borang — borang memerlukannya untuk diterbitkan pada alamat yang berfungsi.", zh: "请先创建工作区，再创建表单——表单需要一个工作区才能在可用地址上发布。", ta: "படிவத்தை உருவாக்குவதற்கு முன் ஒரு பணியிடத்தை உருவாக்குங்கள் — செயல்படும் முகவரியில் வெளியிட படிவத்திற்கு ஒரு பணியிடம் தேவை.", ar: "أنشئ مساحة عمل قبل إنشاء نموذج — يحتاج النموذج إلى مساحة عمل ليُنشر على عنوان يعمل." },
  "apiErr.manageRequired": { en: "You need the manage or admin role in this workspace for that.", ms: "Anda perlukan peranan manage atau admin dalam workspace ini.", zh: "该操作需要此工作区的 manage 或 admin 角色。", ta: "இதற்கு இந்த பணியிடத்தில் manage அல்லது admin பங்கு தேவை.", ar: "تحتاج إلى دور manage أو admin في مساحة العمل هذه." },
  "apiErr.adminRequired": { en: "Only a workspace admin can do that.", ms: "Hanya admin workspace boleh melakukannya.", zh: "只有工作区管理员可以执行该操作。", ta: "பணியிட நிர்வாகி மட்டுமே இதைச் செய்ய முடியும்.", ar: "يمكن لمسؤول مساحة العمل فقط القيام بذلك." },
  "apiErr.scopeReadonly": { en: "This API key can only read. It needs the readwrite scope for that.", ms: "Kunci API ini hanya boleh baca — ia perlukan skop readwrite.", zh: "此 API 密钥仅限读取——该操作需要 readwrite 权限范围。", ta: "இந்த API விசை படிக்க மட்டுமே — இதற்கு readwrite அனுமதி தேவை.", ar: "مفتاح API هذا للقراءة فقط — يحتاج إلى نطاق readwrite." },
  "apiErr.deviceScopeInvalid": { en: "That access level is not available. Pick one of the two options and approve again.", ms: "Tahap akses itu tidak tersedia. Pilih salah satu daripada dua pilihan dan luluskan semula.", zh: "该访问级别不可用。请从两个选项中选择一个并再次批准。", ta: "அந்த அணுகல் நிலை கிடைக்கவில்லை. இரண்டு விருப்பங்களில் ஒன்றைத் தேர்ந்தெடுத்து மீண்டும் அனுமதிக்கவும்.", ar: "مستوى الوصول هذا غير متاح. اختر أحد الخيارين ووافق مرة أخرى." },
  "apiErr.paymentNotConfigured": { en: "Online payments are not set up for this workspace yet.", ms: "Pembayaran dalam talian belum disediakan untuk workspace ini.", zh: "此工作区尚未设置在线支付。", ta: "இந்த பணியிடத்திற்கு ஆன்லைன் பணம் இன்னும் அமைக்கப்படவில்லை.", ar: "الدفع الإلكتروني غير مُهيَّأ لمساحة العمل هذه بعد." },
  "apiErr.paymentInactive": { en: "Payments are inactive for this workspace.", ms: "Pembayaran tidak aktif untuk workspace ini.", zh: "此工作区的支付功能未启用。", ta: "இந்த பணியிடத்திற்கு பணம் செயலில் இல்லை.", ar: "الدفع غير مُفعَّل لمساحة العمل هذه." },
  "apiErr.chipCredentials": { en: "Those payment credentials were rejected. Check the Brand ID and Secret Key.", ms: "Kredensial pembayaran ditolak. Semak Brand ID dan Secret Key.", zh: "支付凭据被拒绝。请检查 Brand ID 和 Secret Key。", ta: "பணம் செலுத்தும் சான்றுகள் நிராகரிக்கப்பட்டன. Brand ID மற்றும் Secret Key சரிபார்க்கவும்.", ar: "تم رفض بيانات اعتماد الدفع. تحقق من Brand ID و Secret Key." },
  "apiErr.refundNotAllowed": { en: "This sale is not in a refundable state.", ms: "Jualan ini bukan dalam status boleh dipulangkan.", zh: "该笔销售当前不可退款。", ta: "இந்த விற்பனை பணம் திருப்பித் தரக்கூடிய நிலையில் இல்லை.", ar: "عملية البيع هذه ليست في حالة قابلة للاسترداد." },
  "apiErr.subscriptionCancelled": { en: "That subscription has already been cancelled.", ms: "Langganan itu telah dibatalkan.", zh: "该订阅已取消。", ta: "அந்த சந்தா ஏற்கனவே ரத்து செய்யப்பட்டது.", ar: "تم إلغاء هذا الاشتراك بالفعل." },
  "apiErr.formStatusInvalid": { en: "Status must be one of: draft, private, published.", ms: "Status mesti salah satu daripada: draft, private, published.", zh: "状态必须为：draft、private、published。", ta: "நிலை இவற்றில் ஒன்றாக இருக்க வேண்டும்: draft, private, published.", ar: "يجب أن تكون الحالة: draft أو private أو published." },
  "apiErr.formPasswordTooShort": { en: "The form password must be at least 4 characters.", ms: "Kata laluan borang mesti sekurang-kurangnya 4 aksara.", zh: "表单密码至少需要 4 个字符。", ta: "படிவ கடவுச்சொல் குறைந்தது 4 எழுத்துகள் இருக்க வேண்டும்.", ar: "يجب أن تتكون كلمة مرور النموذج من 4 أحرف على الأقل." },
  "apiErr.productOutOfStock": { en: "That product is out of stock.", ms: "Produk itu telah habis stok.", zh: "该商品已缺货。", ta: "அந்த தயாரிப்பு இருப்பு தீர்ந்துவிட்டது.", ar: "هذا المنتج غير متوفر في المخزون." },
  "apiErr.templateNeedMetadata": { en: "To publish a template to the gallery it needs a category and one description block (a Section, or a text block).", ms: "Untuk menerbitkan templat ke galeri, ia perlu category dan satu blok description (blok Section, atau blok text).", zh: "将模板发布到画廊需要设置 category 和一个 description 区块（Section 区块，或 text 区块）。", ta: "ஒரு வார்ப்புருவை காட்சியகத்தில் வெளியிட category மற்றும் ஒரு description தொகுதி (Section தொகுதி அல்லது text தொகுதி) தேவை.", ar: "لنشر قالب في المعرض يلزم فئة (category) وكتلة description واحدة (كتلة Section أو كتلة text)." },
  "apiErr.webhookUrlInvalid": { en: "That webhook URL points at a private or loopback host. Use a public address.", ms: "URL webhook itu menunjuk ke host peribadi/loopback — gunakan alamat awam.", zh: "该 Webhook 链接指向私有或回环主机——请使用公网地址。", ta: "அந்த webhook URL தனிப்பட்ட/loopback ஹோஸ்ட்டை குறிக்கிறது — பொது முகவரியைப் பயன்படுத்தவும்.", ar: "يشير رابط خطاف الويب إلى مضيف خاص أو loopback — استخدم عنواناً عاماً." },
  "apiErr.storageQuota": { en: "This workspace has reached its 1GB storage limit.", ms: "Workspace ini telah mencapai had storan 1GB.", zh: "此工作区已达到 1GB 存储上限。", ta: "இந்த பணியிடம் 1GB சேமிப்பு வரம்பை அடைந்துவிட்டது.", ar: "وصلت مساحة العمل هذه إلى حد التخزين 1GB." },
  "apiErr.uploadTypeUnsupported": { en: "That file type is not supported.", ms: "Jenis fail itu tidak disokong.", zh: "不支持该文件类型。", ta: "அந்த கோப்பு வகை ஆதரிக்கப்படவில்லை.", ar: "نوع الملف هذا غير مدعوم." },
  // t_20af3915: an SVG is served as image/svg+xml, so the browser parses it as
  // XML. A file with text before the root element (a `/* */` banner on a
  // hand-written SVG) is a fatal parse error and would never display, so the
  // upload is refused. The sentence names the fix rather than the rule.
  "apiErr.uploadSvgInvalid": { en: "That SVG is not valid XML, so it would not display. Remove any text before the <svg> tag (for example a /* */ banner) and upload it again.", ms: "SVG itu bukan XML yang sah, jadi ia tidak akan dipaparkan. Buang sebarang teks sebelum tag <svg> (contohnya banner /* */) dan muat naik semula.", zh: "该 SVG 不是有效的 XML，因此无法显示。请删除 <svg> 标签之前的任何文本（例如 /* */ 横幅）后重新上传。", ta: "அந்த SVG செல்லுபடியாகும் XML அல்ல, எனவே காட்சிப்படுத்தப்படாது. <svg> குறிச்சொல்லுக்கு முன் உள்ள உரையை (எ.கா. /* */ பேனர்) நீக்கி மீண்டும் பதிவேற்றவும்.", ar: "ملف SVG هذا ليس XML صالحًا، لذا لن يُعرض. احذف أي نص قبل وسم <svg> (مثل لافتة /* */) ثم ارفعه مرة أخرى." },
  "apiErr.certInvalid": { en: "The certificate or key is invalid. Check the PEM format and that the key matches.", ms: "Sijil atau kunci tidak sah — semak format PEM dan padanan kunci.", zh: "证书或密钥无效——请检查 PEM 格式及密钥是否匹配。", ta: "சான்றிதழ் அல்லது விசை தவறானது — PEM வடிவமும் விசைப் பொருத்தமும் சரிபார்க்கவும்.", ar: "الشهادة أو المفتاح غير صالح — تحقق من تنسيق PEM ومن تطابق المفتاح." },
  "apiErr.rateLimited": { en: "Too many attempts. Please try again shortly.", ms: "Terlalu banyak percubaan. Sila cuba sebentar lagi.", zh: "尝试次数过多。请稍后再试。", ta: "அதிக முயற்சிகள். சிறிது நேரத்தில் மீண்டும் முயற்சிக்கவும்.", ar: "محاولات كثيرة جداً. يرجى المحاولة بعد قليل." },
  // The throttle sentence WITH the window the server chose (card t_e50a9680).
  // `apiErr.rateLimited` above stays the answer when the answer carries no
  // usable `Retry-After` — a 429 that cannot name a wait still has to say
  // something better than the page's generic failure line, and reusing that row
  // rather than writing a second "too many attempts" sentence keeps one wording
  // to maintain. These two are the same refusal with the number substituted;
  // the unit is chosen by lib/rateLimitErr.ts (seconds under two minutes,
  // rounded-up whole minutes above it, so "1 minute" can never render).
  "apiErr.tooManyRequestsInMinutes": { en: "Too many attempts. Try again in {minutes} minutes.", ms: "Terlalu banyak percubaan — cuba lagi dalam {minutes} minit.", zh: "尝试次数过多——请在 {minutes} 分钟后重试。", ta: "அதிக முயற்சிகள் — {minutes} நிமிடங்களில் மீண்டும் முயற்சிக்கவும்.", ar: "محاولات كثيرة جداً — حاول مرة أخرى خلال {minutes} دقيقة." },
  "apiErr.tooManyRequestsInSeconds": { en: "Too many attempts. Try again in {seconds} seconds.", ms: "Terlalu banyak percubaan — cuba lagi dalam {seconds} saat.", zh: "尝试次数过多——请在 {seconds} 秒后重试。", ta: "அதிக முயற்சிகள் — {seconds} வினாடிகளில் மீண்டும் முயற்சிக்கவும்.", ar: "محاولات كثيرة جداً — حاول مرة أخرى خلال {seconds} ثانية." },
  "apiErr.emailTaken": { en: "That email is already registered.", ms: "Emel itu sudah didaftarkan.", zh: "该邮箱已注册。", ta: "அந்த மின்னஞ்சல் ஏற்கனவே பதிவு செய்யப்பட்டுள்ளது.", ar: "هذا البريد مسجَّل بالفعل." },
  "apiErr.emailInvalid": { en: "That email address is not valid. Check it for a typo.", ms: "Alamat emel itu tidak sah. Semak sama ada ada salah taip.", zh: "该邮箱地址无效。请检查是否输入有误。", ta: "அந்த மின்னஞ்சல் முகவரி தவறானது. எழுத்துப் பிழை உள்ளதா எனச் சரிபார்க்கவும்.", ar: "عنوان البريد هذا غير صالح. تحقق من عدم وجود خطأ إملائي." },
  "apiErr.emailDomainUnreachable": { en: "That email domain does not accept mail.", ms: "Domain emel itu tidak menerima emel.", zh: "该邮箱域名不接收邮件。", ta: "அந்த மின்னஞ்சல் டொமைன் மின்னஞ்சலை ஏற்கவில்லை.", ar: "نطاق البريد هذا لا يستقبل الرسائل." },
  "apiErr.emailUnverified": { en: "Please verify your email before signing in. Check your inbox for the verification link.", ms: "Sila sahkan emel anda sebelum log masuk — semak peti masuk untuk pautan pengesahan.", zh: "请先验证邮箱再登录——请查收邮箱中的验证链接。", ta: "உள்நுழைவதற்கு முன் மின்னஞ்சலைச் சரிபார்க்கவும் — சரிபார்ப்பு இணைப்புக்காக உங்கள் inbox-ஐப் பார்க்கவும்.", ar: "يرجى تأكيد بريدك قبل تسجيل الدخول — تحقق من صندوق الوارد للحصول على رابط التأكيد." },
  "apiErr.passwordTooShort": { en: "The password must be at least 10 characters.", ms: "Kata laluan mesti sekurang-kurangnya 10 aksara.", zh: "密码至少需要 10 个字符。", ta: "கடவுச்சொல் குறைந்தது 10 எழுத்துகள் இருக்க வேண்டும்.", ar: "يجب أن تتكون كلمة المرور من 10 أحرف على الأقل." },
  "apiErr.passwordInvalid": { en: "That password is incorrect.", ms: "Kata laluan itu salah.", zh: "密码不正确。", ta: "அந்த கடவுச்சொல் தவறானது.", ar: "كلمة المرور هذه غير صحيحة." },
  "apiErr.loginInvalid": { en: "Email or password is incorrect.", ms: "Emel atau kata laluan salah.", zh: "邮箱或密码不正确。", ta: "மின்னஞ்சல் அல்லது கடவுச்சொல் தவறானது.", ar: "البريد الإلكتروني أو كلمة المرور غير صحيحة." },
  "apiErr.stepUpRequired": { en: "Confirm your 2FA code to continue.", ms: "Sahkan kod 2FA anda untuk teruskan.", zh: "请确认您的 2FA 验证码以继续。", ta: "தொடர உங்கள் 2FA குறியீட்டை உறுதிப்படுத்தவும்.", ar: "أكّد رمز التحقق الثنائي للمتابعة." },
  // The three refusals main grew after this branch's base (#682): each one names
  // the obstacle that blocks, so the sentence has to carry it.
  "apiErr.formSlugTaken": {
    en: "This form can't be restored: another form has taken its slug. Change that form's slug first, then restore this one.",
    ms: "Borang ini tidak boleh dipulihkan: borang lain telah mengambil slug-nya. Tukar slug borang itu dahulu, kemudian pulihkan borang ini.",
    zh: "无法恢复此表单：其 Slug 已被其他表单占用。请先修改那个表单的 Slug，然后再恢复此表单。",
    ta: "இந்தப் படிவத்தை மீட்டமைக்க முடியாது: அதன் slug-ஐ வேறு படிவம் எடுத்துக்கொண்டது. முதலில் அந்தப் படிவத்தின் slug-ஐ மாற்றி, பின்னர் இதை மீட்டமைக்கவும்.",
    ar: "تعذّرت استعادة هذا النموذج: نموذج آخر أخذ الـ slug الخاص به. غيّر الـ slug لذلك النموذج أولاً، ثم استعد هذا النموذج.",
  },
  // The two refusals a form-slug RENAME answers with (the Form Settings slug
  // field). Distinct from apiErr.formSlugTaken above on purpose: a restore is
  // unblocked by freeing the OTHER form's slug, a rename by picking a different
  // slug for THIS form, and each sentence has to give its own remedy.
  "apiErr.formSlugInUse": {
    en: "Another form in this workspace is already served at that URL. Choose a different slug.",
    ms: "Borang lain dalam workspace ini sudah menggunakan URL itu. Pilih slug yang lain.",
    zh: "此工作区中的另一个表单已使用该网址。请选择其他 Slug。",
    ta: "இந்தப் பணியிடத்தில் உள்ள மற்றொரு படிவம் ஏற்கனவே அந்த URL-இல் வழங்கப்படுகிறது. வேறு slug-ஐத் தேர்ந்தெடுக்கவும்.",
    ar: "نموذج آخر في مساحة العمل هذه يُقدَّم بالفعل على هذا الرابط. اختر slug مختلفاً.",
  },
  // The reserved-name refusal for a form slug (card t_f76f81eb). A form slug
  // that names something JomForm owns or serves on the same host (login, api,
  // docs, privacy, …) is refused with urlguard's `form.slugReserved` token. The
  // server's own sentence can never be printed here: it is English and the API
  // does not know the dashboard's language (#6177).
  "apiErr.formSlugReserved": {
    en: "This name is reserved: JomForm uses it for its own addresses. Choose a different slug.",
    ms: "Nama ini dikhaskan — JomForm menggunakannya untuk alamatnya sendiri. Pilih slug lain.",
    zh: "此名称已被保留——JomForm 将其用于自己的地址。请选择其他 Slug。",
    ta: "இந்தப் பெயர் ஒதுக்கப்பட்டுள்ளது — JomForm அதை அதன் சொந்த முகவரிகளுக்குப் பயன்படுத்துகிறது. வேறு slug-ஐத் தேர்ந்தெடுக்கவும்.",
    ar: "هذا الاسم محجوز — تستخدمه JomForm لعناوينها الخاصة. اختر slug مختلفاً.",
  },
  "apiErr.formSlugInvalid": {
    en: "A slug must be 1-40 characters of a-z, 0-9 or dashes.",
    ms: "Slug mesti 1-40 aksara daripada a-z, 0-9 atau sengkang.",
    zh: "Slug 必须为 1-40 个字符，仅限 a-z、0-9 或连字符。",
    ta: "slug என்பது a-z, 0-9 அல்லது கோடுகளால் ஆன 1-40 எழுத்துகளாக இருக்க வேண்டும்.",
    ar: "يجب أن يتكون الـ slug من 1-40 حرفاً من a-z أو 0-9 أو الشرطات.",
  },
  // The power-user custom_js scan refusal (#388) — card t_2717ba30.
  //
  // THE BUG THIS ANSWERS. The API's sentence for it is assembled around the
  // scan's own reason ("custom_js ditolak: active code execution marker: eval("),
  // so no message key could carry it, and the 400 shipped as the class code
  // `request.badRequest` — which this dictionary deliberately does not translate.
  // Measured on prod v1.21.296: pasting `eval('2+2');` into /form-settings'
  // "Custom JS (power users)" field and pressing Save rendered the bare word
  // "Failed" under the form. The refusal was right; the sentence named neither
  // the field nor the reason.
  //
  // WHY THE SENTENCE NAMES THE FIELD AND THE PATTERN FAMILIES. The code is one
  // for every door (the form write paths, the template write paths and the MCP
  // tools all write that same sentence), so it cannot carry which of the three
  // scan classes fired: active code execution (`eval(`, `new Function(` …),
  // obfuscation/beacon (`atob(`, `sendBeacon(` …) or a reference to an external
  // URL. Listing the families is the honest answer — the merchant's own script
  // is the only thing that can tell them which one they hit — and the remedy is
  // the same for all three: remove it from the field and save again. Naming the
  // field is the part a generic "Failed" cannot do.
  "apiErr.formCustomJsRejected": {
    en: "Custom JS was rejected: it contains something we do not allow on a live form (code execution, obfuscation or beacon code, or an external URL). Remove it and save again.",
    ms: "Custom JS ditolak: ia mengandungi corak yang tidak dibenarkan pada borang yang disiarkan — pelaksanaan kod, kod penyamaran/beacon, atau URL luaran. Buangnya dan simpan semula.",
    zh: "Custom JS 被拒绝：其中包含我们不允许在已发布表单中使用的模式——代码执行、混淆或信标代码，或外部网址。请移除后重新保存。",
    ta: "Custom JS நிராகரிக்கப்பட்டது: வெளியிடப்பட்ட படிவத்தில் அனுமதிக்கப்படாத ஒன்று (code execution, obfuscation/beacon code அல்லது வெளிப்புற URL) உள்ளது. அதை நீக்கி மீண்டும் சேமிக்கவும்.",
    ar: "تم رفض Custom JS: يحتوي على شيء لا نسمح به في نموذج منشور — تنفيذ كود، أو كود تشويش/beacon، أو رابط خارجي. أزله ثم احفظ مرة أخرى.",
  },
  "apiErr.formHasSales": {
    en: "This form has sales, so it cannot be deleted for good. Sales history must not be destroyed with the form. It stays in the trash.",
    ms: "Borang ini mempunyai jualan, jadi ia tidak boleh dipadam secara kekal — sejarah jualan tidak boleh dimusnahkan bersama borang. Ia kekal dalam tong sampah.",
    zh: "此表单已有销售记录，无法永久删除——销售历史不能随表单一起销毁。它将保留在回收站中。",
    ta: "இந்தப் படிவத்தில் விற்பனைகள் உள்ளன, எனவே நிரந்தரமாக நீக்க முடியாது — விற்பனை வரலாறு படிவத்துடன் அழிக்கப்படக்கூடாது. இது குப்பைத் தொட்டியில் இருக்கும்.",
    ar: "لهذا النموذج مبيعات، لذا لا يمكن حذفه نهائياً — لا يجوز إتلاف سجل المبيعات مع النموذج. سيبقى في سلة المهملات.",
  },
  // The product bin's two refusals, the same class as the form's above. A
  // product's permanent delete is blocked by a SALE (history) or by a live
  // SUBSCRIPTION (a billing commitment the product must keep billing), and each
  // sentence has to name which one so the merchant knows what to clear.
  "apiErr.productHasSales": {
    en: "This product has sales, so it cannot be deleted for good. Sales history must not be destroyed with the product. It stays in the trash.",
    ms: "Produk ini mempunyai jualan, jadi ia tidak boleh dipadam secara kekal — sejarah jualan tidak boleh dimusnahkan bersama produk. Ia kekal dalam tong sampah.",
    zh: "此商品已有销售记录，无法永久删除——销售历史不能随商品一起销毁。它将保留在回收站中。",
    ta: "இந்தத் தயாரிப்பில் விற்பனைகள் உள்ளன, எனவே நிரந்தரமாக நீக்க முடியாது — விற்பனை வரலாறு தயாரிப்புடன் அழிக்கப்படக்கூடாது. இது குப்பைத் தொட்டியில் இருக்கும்.",
    ar: "لهذا المنتج مبيعات، لذا لا يمكن حذفه نهائياً — لا يجوز إتلاف سجل المبيعات مع المنتج. سيبقى في سلة المهملات.",
  },
  "apiErr.productHasSubscriptions": {
    en: "This product still has subscriptions, so it cannot be deleted for good. Cancel them first. It stays in the trash.",
    ms: "Produk ini masih ada langganan, jadi ia tidak boleh dipadam secara kekal — batalkan langganan itu dahulu. Ia kekal dalam tong sampah.",
    zh: "此商品仍有订阅，无法永久删除——请先取消这些订阅。它将保留在回收站中。",
    ta: "இந்தத் தயாரிப்பில் இன்னும் சந்தாக்கள் உள்ளன, எனவே நிரந்தரமாக நீக்க முடியாது — முதலில் அவற்றை ரத்து செய்யவும். இது குப்பைத் தொட்டியில் இருக்கும்.",
    ar: "لا تزال لهذا المنتج اشتراكات، لذا لا يمكن حذفه نهائياً — ألغِها أولاً. سيبقى في سلة المهملات.",
  },
  // A shipping-zone-set save that would delete a set a form's shipping block
  // still points at. The API prose names the set and the forms (for API-key and
  // MCP callers); this sentence gives the DASHBOARD user the remedy, which is
  // the only thing the generic save failure did not say.
  "apiErr.shipSetInUse": {
    en: "A form still uses this shipping zone set, so it can't be removed. Open that form's shipping block, point it at another set (or keep this one), then save again.",
    ms: "Masih ada borang yang menggunakan set zon penghantaran ini, jadi ia tidak boleh dibuang. Buka blok penghantaran borang itu, tukar ke set lain (atau kekalkan set ini), kemudian simpan semula.",
    zh: "仍有表单在使用此运费区域组，因此无法移除。请打开该表单的运费模块，改指向其他区域组（或保留这个），然后再次保存。",
    ta: "இந்த அனுப்பும் மண்டல தொகுப்பை ஒரு படிவம் இன்னும் பயன்படுத்துகிறது, எனவே அதை நீக்க முடியாது. அந்தப் படிவத்தின் அனுப்பும் தொகுதியைத் திறந்து, வேறு தொகுப்புக்கு மாற்றவும் (அல்லது இதை வைத்திருக்கவும்), பின்னர் மீண்டும் சேமிக்கவும்.",
    ar: "لا يزال نموذج يستخدم مجموعة مناطق الشحن هذه، لذا لا يمكن حذفها. افتح كتلة الشحن في ذلك النموذج، ووجّهها إلى مجموعة أخرى (أو أبقِ هذه)، ثم احفظ مرة أخرى.",
  },
  // A shipping-zone-set save the server refused because the CONFIGURATION cannot
  // resolve (card t_417fbacb): a set with no name, a set with no zone, an unnamed
  // zone, a zone with no rate, a negative price, an inverted weight band. The API
  // prose names the exact set/zone (for API-key and MCP callers); this sentence
  // gives the DASHBOARD user the three rules a save has to satisfy, because the
  // page's own "Failed to save shipping rates" said nothing at all.
  //
  // Deliberately ONE sentence for every shape: the remedy is always the same —
  // fix the zone or its rate and save again — and one sentence cannot drift out
  // of step with twelve near-duplicates.
  "apiErr.shipInvalid": {
    en: "The shipping rates were not saved. Every zone set needs a name, each set needs at least one zone, and every zone needs at least one rate. Fix the zone or rate you were editing, then save again.",
    ms: "Kadar penghantaran tidak disimpan. Setiap set zon perlu ada nama, setiap set perlu sekurang-kurangnya satu zon, dan setiap zon perlu sekurang-kurangnya satu kadar. Betulkan zon atau kadar yang anda sedang edit, kemudian simpan semula.",
    zh: "运费未保存。每个区域组都需要名称，每个组至少需要一个区域，每个区域至少需要一条费率。请修正刚才编辑的区域或费率，然后再次保存。",
    ta: "அனுப்பும் கட்டணங்கள் சேமிக்கப்படவில்லை. ஒவ்வொரு மண்டல தொகுப்பிற்கும் பெயர் தேவை, ஒவ்வொரு தொகுப்பிற்கும் குறைந்தது ஒரு மண்டலம் தேவை, ஒவ்வொரு மண்டலத்திற்கும் குறைந்தது ஒரு கட்டணம் தேவை. நீங்கள் திருத்திய மண்டலம் அல்லது கட்டணத்தைச் சரிசெய்து, மீண்டும் சேமிக்கவும்.",
    ar: "لم تُحفظ أسعار الشحن. كل مجموعة مناطق تحتاج إلى اسم، وكل مجموعة تحتاج إلى منطقة واحدة على الأقل، وكل منطقة تحتاج إلى سعر واحد على الأقل. صحّح المنطقة أو السعر الذي كنت تحرره، ثم احفظ مرة أخرى.",
  },
  // The TYPED-CONFIRMATION refusals (the destructive calls that make the caller
  // name back what it is destroying). A DIFFERENT class from the two above:
  // nothing blocks the delete — the caller has simply not confirmed it — so the
  // sentence must ask for the value, not name an obstacle. The dashboard used to
  // fall back to its generic "we couldn't delete that" line for all three,
  // because the server's assembled message could not carry a code.
  "apiErr.productConfirmNameMismatch": {
    en: "The name doesn't match this product. Type the product's own name to delete it for good.",
    ms: "Nama itu tidak sepadan dengan produk ini. Taip nama produk itu sendiri untuk padam secara kekal.",
    zh: "名称与此商品不一致。请输入该商品自身的名称以永久删除。",
    ta: "பெயர் இந்தத் தயாரிப்புடன் பொருந்தவில்லை. நிரந்தரமாக நீக்க அந்தத் தயாரிப்பின் பெயரையே தட்டச்சு செய்யவும்.",
    ar: "الاسم لا يطابق هذا المنتج. اكتب اسم المنتج نفسه لحذفه نهائياً.",
  },
  "apiErr.formConfirmSlugMismatch": {
    en: "The slug doesn't match this form. Type the form's own slug to delete it for good.",
    ms: "Slug itu tidak sepadan dengan borang ini. Taip slug borang itu sendiri untuk padam secara kekal.",
    zh: "Slug 与此表单不一致。请输入该表单自身的 Slug 以永久删除。",
    ta: "slug இந்தப் படிவத்துடன் பொருந்தவில்லை. நிரந்தரமாக நீக்க அந்தப் படிவத்தின் slug-ஐயே தட்டச்சு செய்யவும்.",
    ar: "الـ slug لا يطابق هذا النموذج. اكتب الـ slug الخاص بالنموذج نفسه لحذفه نهائياً.",
  },
  // One sentence for BOTH workspace refusals — the bin and the permanent delete
  // share the code on purpose (the complaint and the remedy are the same), so
  // this sentence must be true of either: it asks for the slug and does not
  // promise which of the two the caller was doing.
  "apiErr.workspaceConfirmSlugMismatch": {
    en: "The slug doesn't match this workspace. Type the workspace's own slug to confirm.",
    ms: "Slug itu tidak sepadan dengan workspace ini. Taip slug workspace itu sendiri untuk mengesahkan.",
    zh: "Slug 与此工作区不一致。请输入该工作区自身的 Slug 以确认。",
    ta: "slug இந்தப் பணியிடத்துடன் பொருந்தவில்லை. உறுதிப்படுத்த அந்தப் பணியிடத்தின் slug-ஐயே தட்டச்சு செய்யவும்.",
    ar: "الـ slug لا يطابق مساحة العمل هذه. اكتب الـ slug الخاص بمساحة العمل نفسها للتأكيد.",
  },
  "apiErr.workspaceNotEmpty": {
    en: "This workspace still has content, so it cannot be deleted. Empty it first; the list below says what is left.",
    ms: "Workspace ini masih mempunyai kandungan, jadi ia tidak boleh dipadam — kosongkannya dahulu (senarai di bawah menunjukkan apa yang tinggal).",
    zh: "此工作区仍有内容，无法删除——请先清空（下方列出剩余内容）。",
    ta: "இந்தப் பணியிடத்தில் இன்னும் உள்ளடக்கம் உள்ளது, எனவே நீக்க முடியாது — முதலில் காலி செய்யவும் (கீழே எஞ்சியவை பட்டியலிடப்பட்டுள்ளன).",
    ar: "ما زالت مساحة العمل هذه تحتوي على محتوى، لذا لا يمكن حذفها — أفرغها أولاً (القائمة أدناه توضح ما تبقّى).",
  },
  "apiErr.workspaceHasSales": {
    en: "This workspace has sales, so it cannot be permanently deleted. Sales are the record of money you received and are never destroyed with a workspace. It stays in the Trash.",
    ms: "Workspace ini mempunyai jualan, jadi ia tidak boleh dipadam secara kekal — jualan ialah rekod wang yang anda terima dan tidak pernah dimusnahkan bersama workspace. Ia kekal dalam Tong Sampah.",
    zh: "此工作区已有销售记录，无法永久删除——销售是您收到款项的记录，绝不会随工作区一起销毁。它将保留在回收站中。",
    ta: "இந்தப் பணியிடத்தில் விற்பனைகள் உள்ளன, எனவே நிரந்தரமாக நீக்க முடியாது — விற்பனை என்பது நீங்கள் பெற்ற பணத்தின் பதிவு; அது பணியிடத்துடன் அழிக்கப்படாது. இது குப்பைத் தொட்டியில் இருக்கும்.",
    ar: "مساحة العمل هذه بها مبيعات، لذا لا يمكن حذفها نهائياً — المبيعات هي سجل الأموال التي استلمتها ولا تُمحى أبداً مع مساحة العمل. ستبقى في سلة المهملات.",
  },
  // The social-connect ownership link (issue #302). Rendered by /connect, which
  // has no page context to fall back on — the emailed link IS the whole request
  // — so a refusal that is not about the account at all (bad or expired link,
  // another account already holding the identity) gets its own sentence.
  "apiErr.connectInvalid": { en: "This connect link is not valid. Open the link from the email we sent you.", ms: "Pautan penghubung ini tidak sah. Buka pautan daripada emel yang kami hantar.", zh: "此关联链接无效。请打开我们发送给您的邮件中的链接。", ta: "இந்த இணைப்பு லிங்க் தவறானது. நாங்கள் அனுப்பிய மின்னஞ்சலில் உள்ள இணைப்பைத் திறக்கவும்.", ar: "رابط الربط هذا غير صالح. افتح الرابط من البريد الذي أرسلناه إليك." },
  "apiErr.connectExpired": { en: "This connect link has expired. Sign in with your social account again to get a fresh one.", ms: "Pautan penghubung ini telah tamat tempoh. Log masuk semula dengan akaun sosial anda untuk mendapatkan pautan baharu.", zh: "此关联链接已过期。请重新使用您的社交账户登录以获取新链接。", ta: "இந்த இணைப்பு லிங்க் காலாவதியானது. புதியதைப் பெற உங்கள் சமூக கணக்கில் மீண்டும் உள்நுழையவும்.", ar: "انتهت صلاحية رابط الربط هذا. سجّل الدخول بحسابك الاجتماعي مرة أخرى للحصول على رابط جديد." },
  "apiErr.connectNoAccount": { en: "The JomForm account this link belongs to no longer exists.", ms: "Akaun JomForm yang memiliki pautan ini tidak lagi wujud.", zh: "此链接所属的 JomForm 账户已不存在。", ta: "இந்த இணைப்பு சேர்ந்த JomForm கணக்கு இனி இல்லை.", ar: "حساب JomForm الذي ينتمي إليه هذا الرابط لم يعد موجوداً." },
  "apiErr.connectIdentityTaken": { en: "This social account is already connected to a different JomForm account.", ms: "Akaun sosial ini sudah dihubungkan dengan akaun JomForm yang lain.", zh: "此社交账户已关联到另一个 JomForm 账户。", ta: "இந்த சமூக கணக்கு ஏற்கனவே வேறு JomForm கணக்குடன் இணைக்கப்பட்டுள்ளது.", ar: "هذا الحساب الاجتماعي مرتبط بالفعل بحساب JomForm آخر." },
  // "No token in the request", one per surface (issue #6177 follow-up): each
  // sentence names the link that is missing, because the API used to answer one
  // shared Malay literal for all four and the page could only say "something
  // went wrong".
  "apiErr.verifyTokenMissing": { en: "This confirmation link is missing its token. Open the link from the email we sent you, or ask for a new one.", ms: "Pautan pengesahan ini tiada tokennya. Buka pautan daripada emel yang kami hantar, atau minta pautan baharu.", zh: "此确认链接缺少令牌。请打开我们发送的邮件中的链接，或重新获取一封。", ta: "இந்த உறுதிப்படுத்தல் இணைப்பில் டோக்கன் இல்லை. நாங்கள் அனுப்பிய மின்னஞ்சலில் உள்ள இணைப்பைத் திறக்கவும், அல்லது புதியதைக் கேளுங்கள்.", ar: "رابط التأكيد هذا تفتقد رمزه. افتح الرابط من البريد الذي أرسلناه إليك، أو اطلب رابطاً جديداً." },
  "apiErr.resetTokenMissing": { en: "This password-reset link is missing its token. Request a new reset link and open it from your email.", ms: "Pautan set semula kata laluan ini tiada tokennya. Minta pautan set semula baharu dan buka daripada emel anda.", zh: "此密码重置链接缺少令牌。请重新申请重置链接，并从邮件中打开。", ta: "இந்த கடவுச்சொல் மீட்டமைப்பு இணைப்பில் டோக்கன் இல்லை. புதிய மீட்டமைப்பு இணைப்பைக் கேட்டு, மின்னஞ்சலில் திறக்கவும்.", ar: "رابط إعادة تعيين كلمة المرور هذا تفتقد رمزه. اطلب رابطاً جديداً وافتحه من بريدك." },
  "apiErr.magicLinkTokenMissing": { en: "This sign-in link is missing its token. Request a new sign-in link and open it from your email.", ms: "Pautan log masuk ini tiada tokennya. Minta pautan log masuk baharu dan buka daripada emel anda.", zh: "此登录链接缺少令牌。请重新申请登录链接，并从邮件中打开。", ta: "இந்த உள்நுழைவு இணைப்பில் டோக்கன் இல்லை. புதிய உள்நுழைவு இணைப்பைக் கேட்டு, மின்னஞ்சலில் திறக்கவும்.", ar: "رابط تسجيل الدخول هذا تفتقد رمزه. اطلب رابطاً جديداً وافتحه من بريدك." },
  "set.lpPrimaryColor": { en: "Accent colour", ms: "Warna aksen", zh: "强调色", ta: "முதன்மை வண்ணம்", ar: "اللون المميز" },
  "set.lpBackgroundColor": { en: "Background colour", ms: "Warna latar", zh: "背景颜色", ta: "பின்னணி வண்ணம்", ar: "لون الخلفية" },
  "set.lpCtas": { en: "CTA buttons", ms: "Butang CTA", zh: "CTA 按钮", ta: "CTA பொத்தான்கள்", ar: "أزرار CTA" },
  "set.lpCtaLabel": { en: "Button label", ms: "Label butang", zh: "按钮文字", ta: "பொத்தான் பெயர்", ar: "نص الزر" },
  "set.lpCtaSlug": { en: "Form slug", ms: "Slug borang", zh: "表单 Slug", ta: "படிவ slug", ar: "نموذج slug" },
  "set.lpCtaAdd": { en: "Add button", ms: "Tambah butang", zh: "添加按钮", ta: "பொத்தான் சேர்", ar: "إضافة زر" },
  "set.lpForms": { en: "Embedded forms", ms: "Borang terbenam", zh: "嵌入表单", ta: "உட்பொதிக்கப்பட்ட படிவங்கள்", ar: "النماذج المضمّنة" },
  "set.lpFormLabel": { en: "Block heading (optional)", ms: "Tajuk blok (pilihan)", zh: "区块标题（可选）", ta: "தொகுதி தலைப்பு (விருப்பம்)", ar: "عنوان المربع (اختياري)" },
  "set.lpFormSlug": { en: "Form slug", ms: "Slug borang", zh: "表单 Slug", ta: "படிவ slug", ar: "نموذج slug" },
  "set.lpFormAdd": { en: "Add form block", ms: "Tambah blok borang", zh: "添加表单块", ta: "படிவத் தொகுதி சேர்", ar: "إضافة مربع نموذج" },
  "set.lpSave": { en: "Save landing page", ms: "Simpan landing page", zh: "保存落地页", ta: "லேண்டிங் பக்கத்தைச் சேமி", ar: "حفظ صفحة الهبوط" },
  "set.lpSaved": { en: "Landing page saved ✓", ms: "Landing page disimpan ✓", zh: "落地页已保存 ✓", ta: "லேண்டிங் பக்கம் சேமிக்கப்பட்டது ✓", ar: "تم حفظ صفحة الهبوط ✓" },
  // The short form for the BUTTON's saved state, as set.sfSavedShort is for
  // the footer. The full "Landing page saved ✓" (set.lpSaved) is the toast text;
  // putting it on the button would widen it mid-press.
  "set.lpSavedShort": { en: "Saved ✓", ms: "Disimpan ✓", zh: "已保存 ✓", ta: "சேமிக்கப்பட்டது ✓", ar: "تم الحفظ ✓" },
  "set.lpPreview": { en: "Preview", ms: "Pratonton", zh: "预览", ta: "மாதிரிக்காட்சி", ar: "معاينة" },
  // Issue #438: QR ordering flow — restaurant-menu landing template.
  "set.menuTitle": { en: "Restaurant menu (QR ordering)", ms: "Menu restoran (pesanan QR)", zh: "餐厅菜单（二维码点餐）", ta: "உணவக மெனு (QR ஆர்டர்)", ar: "قائمة المطعم (طلب QR)" },
  "set.menuHelp": { en: "Turn THIS form into a clean restaurant menu (categories, item images, prices, best-seller). The menu is served at the form's own URL and shows only the products this form's product blocks reference. Customers scan a QR stand at the table, pick items and pay online. The order reaches you with the table number.", ms: "Jadikan BORANG INI menu restoran yang bersih (kategori, imej item, harga, best-seller). Menu dipaparkan di URL borang ini sendiri dan hanya menunjukkan produk yang dirujuk oleh blok produk borang ini. Pelanggan imbas stand QR di meja, pilih item dan bayar dalam talian — pesanan sampai kepada anda dengan nombor meja.", zh: "将此表单变成简洁的餐厅菜单（分类、商品图片、价格、畅销品）。菜单在此表单自身的网址上提供，且仅显示此表单商品区块所引用的商品。顾客扫描桌上的二维码立牌，选择商品并在线支付——订单连同桌号送达给您。", ta: "இந்தப் படிவத்தை சுத்தமான உணவக மெனுவாக மாற்றவும் (வகைகள், பொருள் படங்கள், விலைகள், சிறந்த விற்பனை). மெனு இந்தப் படிவத்தின் சொந்த URL-இல் வழங்கப்படும், மேலும் இந்தப் படிவத்தின் பொருள் தொகுதிகள் குறிப்பிடும் பொருட்களை மட்டுமே காட்டும். வாடிக்கையாளர்கள் மேசையில் QR ஸ்டாண்டை ஸ்கேன் செய்து, பொருட்களைத் தேர்ந்தெடுத்து ஆன்லைனில் செலுத்துகிறார்கள் — ஆர்டர் மேசை எண்ணுடன் உங்களை அடைகிறது.", ar: "حوّل هذا النموذج إلى قائمة مطعم نظيفة (فئات، صور عناصر، أسعار، الأكثر مبيعاً). تُقدَّم القائمة على رابط النموذج نفسه وتعرض فقط المنتجات التي تشير إليها كتل منتجات هذا النموذج. يمسح العملاء رمز QR على الطاولة، يختارون العناصر ويدفعون عبر الإنترنت — يصل الطلب إليك مع رقم الطاولة." },
  "set.menuEnabledTitle": { en: "Enable restaurant menu", ms: "Aktifkan menu restoran", zh: "启用餐厅菜单", ta: "உணவக மெனுவை இயக்கு", ar: "تفعيل قائمة المطعم" },
  "set.menuEnabledHelp": { en: "When on, this form's own URL renders the menu page instead of the normal form. The workspace home page is never affected.", ms: "Apabila diaktifkan, URL borang ini sendiri memaparkan halaman menu dan bukannya borang biasa. Halaman utama workspace tidak terjejas.", zh: "开启后，此表单自身的网址将渲染菜单页而非普通表单。工作区首页不受影响。", ta: "இயக்கத்தில் இருக்கும்போது, இந்தப் படிவத்தின் சொந்த URL சாதாரண படிவத்திற்குப் பதிலாக மெனு பக்கத்தைக் காட்டுகிறது. பணியிட முகப்புப் பக்கம் பாதிக்கப்படாது.", ar: "عند التفعيل، يعرض رابط هذا النموذج نفسه صفحة القائمة بدلاً من النموذج العادي. لا تتأثر الصفحة الرئيسية لمساحة العمل." },
  "set.menuBestSeller": { en: "Best-seller product (optional)", ms: "Produk best-seller (pilihan)", zh: "畅销商品（可选）", ta: "சிறந்த விற்பனை பொருள் (விருப்பம்)", ar: "المنتج الأكثر مبيعاً (اختياري)" },
  "set.menuCategories": { en: "Categories", ms: "Kategori", zh: "分类", ta: "வகைகள்", ar: "الفئات" },
  "set.menuCatName": { en: "Category name", ms: "Nama kategori", zh: "分类名称", ta: "வகை பெயர்", ar: "اسم الفئة" },
  "set.menuCatAdd": { en: "Add category", ms: "Tambah kategori", zh: "添加分类", ta: "வகையைச் சேர்", ar: "إضافة فئة" },
  "set.menuCatRemove": { en: "Remove", ms: "Buang", zh: "移除", ta: "அகற்று", ar: "إزالة" },
  "set.menuPreview": { en: "Preview menu", ms: "Pratonton menu", zh: "预览菜单", ta: "மெனு மாதிரிக்காட்சி", ar: "معاينة القائمة" },
  // Issue #486: product picker for menu categories + best-seller (no more raw
  // product-ID text inputs).
  "set.menuBestSellerNone": { en: "No best-seller selected", ms: "Tiada best-seller dipilih", zh: "未选择畅销品", ta: "சிறந்த விற்பனை தேர்ந்தெடுக்கப்படவில்லை", ar: "لم يتم اختيار الأكثر مبيعاً" },
  "set.menuChooseBestSeller": { en: "Choose best-seller", ms: "Pilih best-seller", zh: "选择畅销品", ta: "சிறந்த விற்பனையைத் தேர்ந்தெடு", ar: "اختر الأكثر مبيعاً" },
  "set.menuChangeBestSeller": { en: "Change", ms: "Tukar", zh: "更换", ta: "மாற்று", ar: "تغيير" },
  "set.menuClearBestSeller": { en: "Clear", ms: "Kosongkan", zh: "清除", ta: "அழி", ar: "مسح" },
  "set.menuPickProducts": { en: "Choose products", ms: "Pilih produk", zh: "选择商品", ta: "பொருட்களைத் தேர்ந்தெடு", ar: "اختر المنتجات" },
  "set.menuPickedCount": { en: "{n} product(s) selected", ms: "{n} produk dipilih", zh: "已选择 {n} 个商品", ta: "{n} பொருட்கள் தேர்ந்தெடுக்கப்பட்டன", ar: "تم اختيار {n} منتج" },
  "set.menuNoProducts": { en: "No products yet", ms: "Belum ada produk", zh: "暂无商品", ta: "இன்னும் பொருட்கள் இல்லை", ar: "لا توجد منتجات بعد" },
  "set.menuUnknownProduct": { en: "Unknown product", ms: "Produk tidak diketahui", zh: "未知商品", ta: "தெரியாத பொருள்", ar: "منتج غير معروف" },
  "set.menuPickerTitleCategory": { en: "Pick products for this category", ms: "Pilih produk untuk kategori ini", zh: "为此分类选择商品", ta: "இந்த வகைக்கு பொருட்களைத் தேர்ந்தெடு", ar: "اختر منتجات لهذه الفئة" },
  "set.menuPickerTitleBestSeller": { en: "Pick the best-seller product", ms: "Pilih produk best-seller", zh: "选择畅销商品", ta: "சிறந்த விற்பனை பொருளைத் தேர்ந்தெடு", ar: "اختر المنتج الأكثر مبيعاً" },
  "set.menuPickerSearch": { en: "Search products…", ms: "Cari produk…", zh: "搜索商品…", ta: "பொருட்களைத் தேடு…", ar: "ابحث عن المنتجات…" },
  "set.menuPickerLoading": { en: "Loading…", ms: "Memuatkan…", zh: "加载中…", ta: "ஏற்றுகிறது…", ar: "جارٍ التحميل…" },
  "set.menuPickerNoProducts": { en: "No active products in this workspace yet.", ms: "Belum ada produk aktif dalam workspace ini.", zh: "此工作区暂无已上架商品。", ta: "இந்த பணியிடத்தில் இன்னும் செயலில் உள்ள பொருட்கள் இல்லை.", ar: "لا توجد منتجات نشطة في مساحة العمل هذه بعد." },
  "set.menuPickerEmpty": { en: "No products match your search.", ms: "Tiada produk sepadan dengan carian anda.", zh: "没有匹配的商品。", ta: "உங்கள் தேடலுக்குப் பொருந்தும் பொருட்கள் இல்லை.", ar: "لا توجد منتجات مطابقة لبحثك." },
  "set.menuPickerSelected": { en: "{n} selected", ms: "{n} dipilih", zh: "已选 {n}", ta: "{n} தேர்ந்தெடுக்கப்பட்டது", ar: "تم اختيار {n}" },
  "set.menuPickerClear": { en: "Clear", ms: "Kosongkan", zh: "清空", ta: "அழி", ar: "مسح" },
  "set.menuPickerCancel": { en: "Cancel", ms: "Batal", zh: "取消", ta: "ரத்து", ar: "إلغاء" },
  "set.menuPickerConfirm": { en: "Done", ms: "Selesai", zh: "完成", ta: "முடிந்தது", ar: "تم" },
  "set.menuPreviewTitle": { en: "Menu preview", ms: "Pratonton menu", zh: "菜单预览", ta: "மெனு மாதிரிக்காட்சி", ar: "معاينة القائمة" },
  "set.menuPreviewHelp": { en: "How your storefront menu will be structured: sections in order, with the items you picked.", ms: "Bagaimana menu storefront anda akan disusun — seksyen mengikut urutan, dengan item yang anda pilih.", zh: "您的店铺菜单将如何组织——按顺序显示各分区及您选择的商品。", ta: "உங்கள் கடை மெனு எவ்வாறு அமைக்கப்படும் — தேர்ந்தெடுத்த பொருட்களுடன் வரிசைப்படி பிரிவுகள்.", ar: "كيف ستُبنى قائمة متجرك — الأقسام بالترتيب مع العناصر التي اخترتها." },
  "set.menuPreviewBestSeller": { en: "Best-seller", ms: "Best-seller", zh: "畅销品", ta: "சிறந்த விற்பனை", ar: "الأكثر مبيعاً" },
  "set.menuPreviewOther": { en: "Uncategorised items", ms: "Item tanpa kategori", zh: "未分类商品", ta: "வகைப்படுத்தப்படாத பொருட்கள்", ar: "عناصر بدون فئة" },
  "set.menuPreviewEmpty": { en: "No categories configured yet. The products this form references show under \"Uncategorised items\".", ms: "Belum ada kategori dikonfigurasi — produk yang dirujuk borang ini dipaparkan di bawah “Item tanpa kategori”.", zh: "尚未配置分类——此表单引用的商品将显示在“未分类商品”下。", ta: "இன்னும் வகைகள் அமைக்கப்படவில்லை — இந்தப் படிவம் குறிப்பிடும் பொருட்கள் “வகைப்படுத்தப்படாத பொருட்கள்” கீழ் காட்டப்படும்.", ar: "لم يتم إعداد أي فئات بعد — تظهر المنتجات التي يشير إليها هذا النموذج تحت «عناصر بدون فئة»." },
  "set.menuPreviewNoProducts": { en: "Add product blocks to this form to see the menu preview.", ms: "Tambah blok produk pada borang ini untuk melihat pratonton menu.", zh: "为此表单添加商品区块以查看菜单预览。", ta: "மெனு மாதிரிக்காட்சியைப் பார்க்க இந்தப் படிவத்தில் பொருள் தொகுதிகளைச் சேர்க்கவும்.", ar: "أضف كتل منتجات إلى هذا النموذج لرؤية معاينة القائمة." },
  "set.menuPreviewItems": { en: "{n} item(s)", ms: "{n} item", zh: "{n} 个商品", ta: "{n} பொருட்கள்", ar: "{n} عنصر" },
  "set.menuDocs": { en: "Learn more", ms: "Ketahui lanjut", zh: "了解更多", ta: "மேலும் அறிக", ar: "اعرف المزيد" },
  // Issue #493: optional order form for extra menu-checkout fields.
  "set.menuOrderForm": { en: "Extra checkout fields (optional order form)", ms: "Medan tambahan semasa bayar (borang pesanan pilihan)", zh: "额外结账字段（可选订单表单）", ta: "கூடுதல் செக்அவுட் புலங்கள் (விருப்ப ஆர்டர் படிவம்)", ar: "حقول دفع إضافية (نموذج طلب اختياري)" },
  "set.menuOrderFormNone": { en: "None. Collect name + email only", ms: "Tiada — kumpul nama + emel sahaja", zh: "无 — 仅收集姓名和电子邮件", ta: "எதுவுமில்லை — பெயர் + மின்னஞ்சல் மட்டும்", ar: "لا شيء — الاسم والبريد الإلكتروني فقط" },
  "set.menuOrderFormHelp": { en: "Pick a published form to collect extra info per order (delivery address, phone, spice level, notes, pickup time). The form's own fields render in the menu checkout and are validated on the server, so there is no second field editor. Create or edit the fields in the form builder.", ms: "Pilih borang diterbitkan untuk kumpul maklumat tambahan bagi setiap pesanan (alamat penghantaran, telefon, tahap kepedasan, nota, masa ambil…). Medan borang itu dipaparkan dalam checkout menu dan disahkan di pelayan — tiada editor medan kedua. Cipta atau edit medan dalam pembina borang.", zh: "选择一个已发布的表单，为每笔订单收集额外信息（配送地址、电话、辣度、备注、取餐时间……）。该表单自身的字段会显示在菜单结账中并在服务器端验证——没有第二个字段编辑器。请在表单构建器中创建或编辑这些字段。", ta: "ஒவ்வொரு ஆர்டருக்கும் கூடுதல் தகவல் (டெலிவரி முகவரி, தொலைபேசி, காரம் அளவு, குறிப்புகள், எடுக்கும் நேரம்…) சேகரிக்க ஒரு வெளியிடப்பட்ட படிவத்தைத் தேர்ந்தெடுக்கவும். அந்தப் படிவத்தின் புலங்கள் மெனு செக்அவுட்டில் காட்டப்பட்டு சர்வரில் சரிபார்க்கப்படும் — இரண்டாவது புல எடிட்டர் இல்லை. புலங்களைப் படிவ பில்டரில் உருவாக்கவும் அல்லது திருத்தவும்.", ar: "اختر نموذجاً منشوراً لجمع معلومات إضافية لكل طلب (عنوان التوصيل، الهاتف، درجة الحرارة، ملاحظات، وقت الاستلام…). تُعرض حقول النموذج نفسه في دفع القائمة ويتم التحقق منها على الخادم — لا يوجد محرر حقول ثانٍ. أنشئ الحقول أو عدّلها في منشئ النماذج." },
  "set.menuOrderFormMandatory": { en: "The buyer's name and email are always collected and cannot be removed. CHIP requires both to create a payment.", ms: "Nama dan emel pembeli sentiasa dikumpul dan tidak boleh dibuang — CHIP memerlukan kedua-duanya untuk mencipta pembayaran.", zh: "买家的姓名和电子邮件始终会被收集，无法移除——CHIP 创建付款时必须同时具备这两项。", ta: "வாடிக்கையாளரின் பெயரும் மின்னஞ்சலும் எப்போதும் சேகரிக்கப்படும், நீக்க முடியாது — கட்டணத்தை உருவாக்க CHIP இரண்டையும் கேட்கிறது.", ar: "يُجمع اسم المشتري وبريده الإلكتروني دائماً ولا يمكن إزالتهما — يتطلب CHIP كليهما لإنشاء الدفع." },
  "set.menuOrderFormSelected": { en: "Fields from: {n}", ms: "Medan dari: {n}", zh: "字段来源：{n}", ta: "புலங்கள்: {n}", ar: "الحقول من: {n}" },
  "set.menuOrderFormMissing": { en: "The selected form is not published. Pick another or publish it.", ms: "Borang yang dipilih tidak diterbitkan — pilih yang lain atau terbitkannya.", zh: "所选表单未发布——请另选一个或将其发布。", ta: "தேர்ந்தெடுத்த படிவம் வெளியிடப்படவில்லை — வேறொன்றைத் தேர்ந்தெடுக்கவும் அல்லது வெளியிடவும்.", ar: "النموذج المحدد غير منشور — اختر نموذجاً آخر أو انشره." },
  "set.menuOrderFormEdit": { en: "Edit fields in builder", ms: "Edit medan dalam pembina", zh: "在构建器中编辑字段", ta: "பில்டரில் புலங்களைத் திருத்து", ar: "تعديل الحقول في المنشئ" },
  // issue #184: per-workspace hosted legal pages (privacy/terms/refund)
  "set.tab.legal": { en: "Legal", ms: "Undang-undang", zh: "法律", ta: "சட்டம்", ar: "قانوني" },
  "set.tab.affiliates": { en: "Affiliates", ms: "Affiliate", zh: "联盟", ta: "இணைப்பாளர்கள்", ar: "الشركاء" },
  "set.affiliatesDocs": { en: "Affiliate marketing guide", ms: "Panduan pemasaran affiliate", zh: "联盟营销指南", ta: "இணைப்பு சந்தைப்படுத்தல் வழிகாட்டி", ar: "دليل التسويق بالعمولة" },
  "set.affiliatesTitle": { en: "Affiliate marketing", ms: "Pemasaran affiliate", zh: "联盟营销", ta: "இணைப்பு சந்தைப்படுத்தல்", ar: "التسويق بالعمولة" },
  "set.affiliatesHelp": { en: "Create affiliate links (?aff=CODE) for each affiliate. Sales driven by an affiliate are attributed to them and commission is computed on paid sales.", ms: "Cipta pautan affiliate (?aff=CODE) untuk setiap affiliate. Jualan yang dibawa affiliate dikreditkan kepadanya dan komisen dikira pada jualan berbayar.", zh: "为每个联盟成员创建联盟链接（?aff=CODE）。联盟带来的销售归其所有，佣金按已付款销售计算。", ta: "ஒவ்வொரு இணைப்பாளருக்கும் இணைப்பு இணைப்புகளை (?aff=CODE) உருவாக்கவும். இணைப்பாளர் கொண்டு வந்த விற்பனைகள் அவருக்கு வரவு வைக்கப்படும், கமிஷன் செலுத்தப்பட்ட விற்பனைகளில் கணக்கிடப்படும்.", ar: "أنشئ روابط إحالة (?aff=CODE) لكل شريك. تُنسب المبيعات التي يحققها الشريك إليه ويُحسب العمولة على المبيعات المدفوعة." },
  "set.affiliateCode": { en: "Code", ms: "Kod", zh: "代码", ta: "குறியீடு", ar: "الرمز" },
  "set.affiliateName": { en: "Name", ms: "Nama", zh: "名称", ta: "பெயர்", ar: "الاسم" },
  "set.affiliateCommission": { en: "Commission %", ms: "Komisen %", zh: "佣金 %", ta: "கமிஷன் %", ar: "العمولة %" },
  "set.affiliateAdd": { en: "Add affiliate", ms: "Tambah affiliate", zh: "添加联盟", ta: "இணைப்பாளரைச் சேர்", ar: "إضافة شريك" },
  "set.affiliateAdded": { en: "Affiliate added ✓", ms: "Affiliate ditambah ✓", zh: "联盟已添加 ✓", ta: "இணைப்பாளர் சேர்க்கப்பட்டது ✓", ar: "تمت إضافة الشريك ✓" },
  "set.affiliatesList": { en: "Affiliates", ms: "Senarai affiliate", zh: "联盟列表", ta: "இணைப்பாளர்கள்", ar: "الشركاء" },
  "set.affiliatesEmpty": { en: "No affiliates yet. Add one above.", ms: "Belum ada affiliate. Tambah di atas.", zh: "暂无联盟。请在上方添加。", ta: "இன்னும் இணைப்பாளர்கள் இல்லை. மேலே சேர்க்கவும்.", ar: "لا يوجد شركاء بعد. أضف واحداً أعلاه." },
  "set.affiliatesLoadFail": { en: "We couldn't load your affiliates. Please try again.", ms: "Kami tidak dapat memuatkan affiliate anda. Sila cuba lagi.", zh: "无法加载您的联盟。请重试。", ta: "உங்கள் இணைப்பாளர்களை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل الشركاء. يرجى المحاولة مرة أخرى." },
  "set.legalTitle": { en: "Legal pages", ms: "Halaman undang-undang", zh: "法律页面", ta: "சட்டப் பக்கங்கள்", ar: "الصفحات القانونية" },
  "set.legalHelp": { en: "Host privacy, terms and refund policies on your storefront ({workspace}.jomform.com/privacy, /terms, /refund). Auto-generate a draft with AI, or write your own.", ms: "Hos polisi privasi, terma dan refund di storefront anda ({workspace}.jomform.com/privacy, /terms, /refund). Jana draf dengan AI, atau tulis sendiri.", zh: "在您的商店托管隐私、条款和退款政策（{workspace}.jomform.com/privacy、/terms、/refund）。用 AI 自动生成草稿，或自行编写。", ta: "உங்கள் கடையில் தனியுரிமை, விதிமுறைகள் மற்றும் பணத்திருப்பக் கொள்கைகளை ஹோஸ்ட் செய்யுங்கள் ({workspace}.jomform.com/privacy, /terms, /refund). AI மூலம் வரைவை உருவாக்கவும், அல்லது நீங்களே எழுதவும்.", ar: "استضف سياسات الخصوصية والشروط والاسترداد في متجرك ({workspace}.jomform.com/privacy و/terms و/refund). أنشئ مسودة بالذكاء الاصطناعي أو اكتبها بنفسك." },
  // Card t_e8b4f7da: the tab that WRITES a legal page and the tab that points a
  // footer LINK at it never mentioned each other, so a merchant could save both
  // and still get no link. This one sentence makes the connection explicit.
  "set.legalFooterPointer": { en: "A page written here appears in the footer of your public forms only when the matching legal URL in Settings → Storefront points at {slug}.jomform.com and this page has content. Otherwise the footer drops the link, and Storefront says why under that field.", ms: "Halaman yang ditulis di sini muncul dalam footer borang awam anda hanya apabila URL undang-undang yang sepadan di Tetapan → Storefront menunjuk ke {slug}.jomform.com dan halaman ini mempunyai kandungan — jika tidak, footer menggugurkan pautan itu, dan Storefront menyatakan sebabnya di bawah medan tersebut.", zh: "此处写好的页面只有在“设置 → 店铺”中对应的法务 URL 指向 {slug}.jomform.com 且本页有内容时，才会出现在公开表单的页脚 — 否则页脚会略去该链接，“店铺”会在该字段下方说明原因。", ta: "இங்கு எழுதப்படும் பக்கம், அமைப்புகள் → Storefront-இல் உள்ள தொடர்புடைய சட்ட URL {slug}.jomform.com-ஐச் சுட்டி, இந்தப் பக்கத்தில் உள்ளடக்கம் இருக்கும்போது மட்டுமே உங்கள் பொது படிவங்களின் கால்பகுதியில் தோன்றும் — இல்லையெனில் கால்பகுதி அந்த இணைப்பை விட்டுவிடும், அதற்கான காரணத்தை Storefront அந்தப் புலத்தின் கீழ் கூறும்.", ar: "لا تظهر الصفحة المكتوبة هنا في تذييل نماذجك العامة إلا عندما يشير رابط قانوني مطابق في الإعدادات ← المتجر إلى {slug}.jomform.com ويكون لهذه الصفحة محتوى — وإلا يحذف التذييل الرابط، ويذكر المتجر السبب أسفل ذلك الحقل." },
  // Card t_48a93084: the hosted legal pages are served from a WORKSPACE
  // SUBDOMAIN (rest_legal.go resolves the workspace from the request host), and
  // a self-hosted install has none — /privacy, /terms and /refund answer 404
  // there. So this variant names no address at all rather than a dead one.
  "set.legalHelpSelf": { en: "Host privacy, terms and refund policies for your storefront. Auto-generate a draft with AI, or write your own.", ms: "Hos polisi privasi, terma dan refund untuk storefront anda. Jana draf dengan AI, atau tulis sendiri.", zh: "为您的商店托管隐私、条款和退款政策。用 AI 自动生成草稿，或自行编写。", ta: "உங்கள் கடைக்கான தனியுரிமை, விதிமுறைகள் மற்றும் பணத்திருப்பக் கொள்கைகளை ஹோஸ்ட் செய்யுங்கள். AI மூலம் வரைவை உருவாக்கவும், அல்லது நீங்களே எழுதவும்.", ar: "استضف سياسات الخصوصية والشروط والاسترداد لمتجرك. أنشئ مسودة بالذكاء الاصطناعي أو اكتبها بنفسك." },
  "set.legalPrivacy": { en: "Privacy Policy", ms: "Polisi Privasi", zh: "隐私政策", ta: "தனியுரிமைக் கொள்கை", ar: "سياسة الخصوصية" },
  "set.legalTerms": { en: "Terms of Service", ms: "Terma & Syarat", zh: "服务条款", ta: "சேவை விதிமுறைகள்", ar: "شروط الخدمة" },
  "set.legalRefund": { en: "Refund Policy", ms: "Polisi Refund", zh: "退款政策", ta: "பணத்திருப்பக் கொள்கை", ar: "سياسة الاسترداد" },
  "set.legalAuto": { en: "Auto-generate", ms: "Jana automatik", zh: "自动生成", ta: "தானாக உருவாக்கு", ar: "توليد تلقائي" },
  "set.legalManual": { en: "Write manually", ms: "Tulis sendiri", zh: "手动编写", ta: "கைமுறையாக எழுது", ar: "اكتب يدوياً" },
  "set.legalGenerate": { en: "Generate draft", ms: "Jana draf", zh: "生成草稿", ta: "வரைவு உருவாக்கு", ar: "إنشاء مسودة" },
  "set.legalGenerating": { en: "Generating…", ms: "Menjana…", zh: "生成中…", ta: "உருவாக்குகிறது…", ar: "جارٍ الإنشاء…" },
  "set.legalBusinessName": { en: "Business name", ms: "Nama perniagaan", zh: "企业名称", ta: "வணிகப் பெயர்", ar: "اسم النشاط التجاري" },
  "set.legalRegNo": { en: "Registration no.", ms: "No. pendaftaran", zh: "注册号", ta: "பதிவு எண்", ar: "رقم التسجيل" },
  "set.legalContact": { en: "Contact (email/phone)", ms: "Hubungan (emel/telefon)", zh: "联系方式（邮箱/电话）", ta: "தொடர்பு (மின்னஞ்சல்/தொலைபேசி)", ar: "جهة الاتصال (بريد/هاتف)" },
  "set.legalAddress": { en: "Address", ms: "Alamat", zh: "地址", ta: "முகவரி", ar: "العنوان" },
  "set.legalDataCollected": { en: "What data is collected", ms: "Data yang dikutip", zh: "收集哪些数据", ta: "சேகரிக்கப்படும் தரவு", ar: "البيانات التي يتم جمعها" },
  "set.legalPaymentTerms": { en: "Payment terms", ms: "Terma pembayaran", zh: "付款条款", ta: "கட்டண விதிமுறைகள்", ar: "شروط الدفع" },
  "set.legalRefundTerms": { en: "Refund terms", ms: "Terma refund", zh: "退款条款", ta: "பணத்திருப்ப விதிமுறைகள்", ar: "شروط الاسترداد" },
  "set.legalWebsite": { en: "Website", ms: "Laman web", zh: "网站", ta: "இணையதளம்", ar: "الموقع" },
  "set.legalEffectiveDate": { en: "Effective date", ms: "Tarikh berkuat kuasa", zh: "生效日期", ta: "நடைமுறை தேதி", ar: "تاريخ السريان" },
  "set.legalContent": { en: "Content", ms: "Kandungan", zh: "内容", ta: "உள்ளடக்கம்", ar: "المحتوى" },
  "set.legalContentPh": { en: "Write your policy text here…", ms: "Tulis teks polisi anda di sini…", zh: "在此处编写您的政策文本…", ta: "உங்கள் கொள்கை உரையை இங்கே எழுதுங்கள்…", ar: "اكتب نص سياستك هنا…" },
  "set.legalSave": { en: "Save page", ms: "Simpan halaman", zh: "保存页面", ta: "பக்கத்தைச் சேமி", ar: "حفظ الصفحة" },
  "set.legalSaved": { en: "Legal page saved ✓", ms: "Halaman undang-undang disimpan ✓", zh: "法律页面已保存 ✓", ta: "சட்டப் பக்கம் சேமிக்கப்பட்டது ✓", ar: "تم حفظ الصفحة القانونية ✓" },
  "set.legalNotSet": { en: "Not set", ms: "Belum ditetapkan", zh: "未设置", ta: "அமைக்கப்படவில்லை", ar: "غير مضبوط" },
  "set.legalSet": { en: "Set", ms: "Ditetapkan", zh: "已设置", ta: "அமைக்கப்பட்டது", ar: "مضبوط" },
  "set.legalModeAuto": { en: "AI-generated", ms: "Dijana AI", zh: "AI 生成", ta: "AI உருவாக்கியது", ar: "منشأ بالذكاء الاصطناعي" },
  "set.legalModeManual": { en: "Manual", ms: "Manual", zh: "手动", ta: "கைமுறை", ar: "يدوي" },
  "set.legalView": { en: "View page ↗", ms: "Lihat halaman ↗", zh: "查看页面 ↗", ta: "பக்கத்தைப் பார்க்க ↗", ar: "عرض الصفحة ↗" },
  "set.legalGenerateFail": { en: "Failed to generate draft.", ms: "Gagal jana draf.", zh: "生成草稿失败。", ta: "வரைவு உருவாக்க முடியவில்லை.", ar: "فشل إنشاء المسودة." },
  "set.legalSaveFail": { en: "Failed to save page.", ms: "Gagal simpan halaman.", zh: "保存页面失败。", ta: "பக்கத்தைச் சேமிக்க முடியவில்லை.", ar: "فشل حفظ الصفحة." },
  "set.legalLoadFail": { en: "Failed to load legal pages.", ms: "Gagal muat halaman undang-undang.", zh: "加载法律页面失败。", ta: "சட்டப் பக்கங்களை ஏற்ற முடியவில்லை.", ar: "فشل تحميل الصفحات القانونية." },
  "set.cfLinkFailed": { en: "Cloudflare linking failed. Please try again.", ms: "Pautan Cloudflare gagal — sila cuba lagi.", zh: "Cloudflare 关联失败 — 请重试。", ta: "Cloudflare இணைப்பு தோல்வி — மீண்டும் முயற்சிக்கவும்.", ar: "فشل ربط Cloudflare — يرجى المحاولة مرة أخرى." },
  "set.cfConnect": { en: "Connect", ms: "Sambung", zh: "连接", ta: "இணைக்க", ar: "اتصال" },
  "set.cfConnecting": { en: "Connecting…", ms: "Menyambung…", zh: "连接中…", ta: "இணைக்கிறது…", ar: "جارٍ الاتصال…" },
  "set.cfManualErr": { en: "Could not connect: ", ms: "Gagal menyambung: ", zh: "无法连接：", ta: "இணைக்க முடியவில்லை: ", ar: "تعذر الاتصال: " },
  // issue #128: webhook event selection + edit
  "webhook.eventsTitle": { en: "Subscribe to events", ms: "Langgan peristiwa", zh: "订阅事件", ta: "நிகழ்வுகளுக்குப் பதிவுசெய்ய", ar: "الاشتراك في الأحداث" },
  "webhook.event.sale.paid": { en: "Sale paid", ms: "Jualan dibayar", zh: "订单已支付", ta: "விற்பனை செலுத்தப்பட்டது", ar: "دفعت عملية البيع" },
  "webhook.event.sale.failed": { en: "The buyer's payment did not go through", ms: "Bayaran pembeli tidak berjaya", zh: "买家的付款未成功", ta: "வாங்குபவரின் கட்டணம் நிறைவேறவில்லை", ar: "لم تنجح عملية دفع المشتري" },
  "webhook.event.subscription.charged": { en: "Subscription charged", ms: "Langganan dicaj", zh: "订阅已扣费", ta: "சந்தா வசூலிக்கப்பட்டது", ar: "اشتراك مسدد" },
  "webhook.event.all": { en: "All events", ms: "Semua peristiwa", zh: "所有事件", ta: "அனைத்து நிகழ்வுகளும்", ar: "كل الأحداث" },
  "set.webhookEdit": { en: "Edit", ms: "Edit", zh: "编辑", ta: "திருத்து", ar: "تحرير" },
  "set.webhookEditTitle": { en: "Edit webhook", ms: "Edit webhook", zh: "编辑 Webhook", ta: "வெப்ஹூக்கைத் திருத்து", ar: "تحرير خطاف الويب" },
  "set.secretKeep": { en: "Signing secret (leave blank to keep current)", ms: "Rahsia tandatangan (kosong = kekal sedia ada)", zh: "签名密钥（留空保持不变）", ta: "கையொப்ப ரகசியம் (காலியாக = தற்போதையதை விடு)", ar: "سر التوقيع (اتركه فارغاً للاحتفاظ بالحالي)" },
  "set.whSave": { en: "Save webhook", ms: "Simpan webhook", zh: "保存 Webhook", ta: "வெப்ஹூக்கைச் சேமி", ar: "حفظ خطاف الويب" },
  "set.whCancel": { en: "Cancel", ms: "Batal", zh: "取消", ta: "ரத்து", ar: "إلغاء" },
  "set.whUpdated": { en: "Webhook updated ✓", ms: "Webhook dikemas kini ✓", zh: "Webhook 已更新 ✓", ta: "வெப்ஹூக் புதுப்பிக்கப்பட்டது ✓", ar: "تم تحديث خطاف الويب ✓" },
  "set.whAddFail": { en: "Failed to add webhook: {error}", ms: "Gagal tambah webhook: {error}", zh: "添加 Webhook 失败：{error}", ta: "வெப்ஹூக் சேர்க்கத் தோல்வி: {error}", ar: "فشل إضافة خطاف الويب: {error}" },
  "set.whUpdateFail": { en: "Failed to update webhook: {error}", ms: "Gagal kemas kini webhook: {error}", zh: "更新 Webhook 失败：{error}", ta: "வெப்ஹூக் புதுப்பிக்கத் தோல்வி: {error}", ar: "فشل تحديث خطاف الويب: {error}" },
  // issue #130: webhook delivery log (status indicator + history)
  "wh.deliveries": { en: "Deliveries", ms: "Penghantaran", zh: "投递记录", ta: "விநியோகங்கள்", ar: "عمليات التسليم" },
  "wh.lastAttempt": { en: "Last attempt", ms: "Cubaan terakhir", zh: "最近投递", ta: "கடைசி முயற்சி", ar: "آخر محاولة" },
  "wh.attemptCount": { en: "{n} attempt(s)", ms: "{n} cubaan", zh: "{n} 次尝试", ta: "{n} முயற்சி(கள்)", ar: "{n} محاولة" },
  "wh.success": { en: "Success", ms: "Berjaya", zh: "成功", ta: "வெற்றி", ar: "نجاح" },
  "wh.failed": { en: "Not delivered", ms: "Tidak dihantar", zh: "未投递", ta: "வழங்கப்படவில்லை", ar: "لم يُسلَّم" },
  "wh.pending": { en: "Pending retry", ms: "Menunggu retry", zh: "等待重试", ta: "மறு முயற்சி காத்திருக்கிறது", ar: "في انتظار إعادة المحاولة" },
  "wh.history": { en: "Delivery history", ms: "Sejarah penghantaran", zh: "投递历史", ta: "விநியோக வரலாறு", ar: "سجل التسليم" },
  "wh.showHistory": { en: "Show history", ms: "Lihat sejarah", zh: "查看历史", ta: "வரலாற்றைக் காட்டு", ar: "عرض السجل" },
  "wh.hideHistory": { en: "Hide history", ms: "Sorok sejarah", zh: "隐藏历史", ta: "வரலாற்றை மறை", ar: "إخفاء السجل" },
  "wh.noDeliveries": { en: "No deliveries yet", ms: "Belum ada penghantaran", zh: "暂无投递", ta: "இன்னும் விநியோகங்கள் இல்லை", ar: "لا عمليات تسليم بعد" },
  "wh.colTime": { en: "Time", ms: "Masa", zh: "时间", ta: "நேரம்", ar: "الوقت" },
  "wh.colEvent": { en: "Event", ms: "Peristiwa", zh: "事件", ta: "நிகழ்வு", ar: "الحدث" },
  "wh.colStatus": { en: "Status", ms: "Status", zh: "状态", ta: "நிலை", ar: "الحالة" },
  "wh.colError": { en: "Error", ms: "Ralat", zh: "错误", ta: "பிழை", ar: "الخطأ" },
  "wh.retryNote": { en: "Failed deliveries are retried automatically with exponential backoff (1m/5m/30m/1h/6h, up to 6 attempts). You can also retry any failed delivery manually.", ms: "Penghantaran gagal akan dicuba semula automatik dengan backoff eksponen (1m/5m/30m/1j/6j, sehingga 6 cubaan). Anda juga boleh cuba semula mana-mana penghantaran gagal secara manual.", zh: "失败的投递会自动以指数退避重试（1m/5m/30m/1h/6h，最多 6 次）。您也可以手动重试任何失败的投递。", ta: "தோல்வியடைந்த விநியோகங்கள் அதிவேக பின்னடைவுடன் தானாக மீண்டும் முயற்சிக்கப்படும் (1m/5m/30m/1h/6h, அதிகபட்சம் 6 முயற்சிகள்). தோல்வியடைந்த எந்த விநியோகத்தையும் கைமுறையாக மீண்டும் முயற்சிக்கலாம்.", ar: "تتم إعادة محاولة عمليات التسليم الفاشلة تلقائياً بفترة انتظار أسية (1د/5د/30د/1س/6س، حتى 6 محاولات). يمكنك أيضاً إعادة محاولة أي تسليم فاشل يدوياً." },
  "wh.retry": { en: "Retry", ms: "Cuba semula", zh: "重试", ta: "மீண்டும் முயற்சி", ar: "إعادة المحاولة" },
  "wh.colAction": { en: "Action", ms: "Tindakan", zh: "操作", ta: "செயல்", ar: "إجراء" },
  "wh.retryQueued": { en: "Retry queued. It will be re-delivered shortly.", ms: "Retry dienqueue — akan dihantar semula sebentar lagi.", zh: "重试已排队 — 将很快重新投递。", ta: "மறு முயற்சி வரிசையில் — விரைவில் மீண்டும் விநியோகிக்கப்படும்.", ar: "تمت جدولة إعادة المحاولة — ستتم إعادة التسليم قريباً." },
  "wh.retryFail": { en: "Failed to queue retry.", ms: "Gagal enqueue retry.", zh: "重试排队失败。", ta: "மறு முயற்சியை வரிசைப்படுத்த முடியவில்லை.", ar: "فشل جدولة إعادة المحاولة." },
  // profile
  "prof.title": { en: "Profile", ms: "Profil", zh: "个人资料", ta: "சுயவிவரம்", ar: "الملف الشخصي" },
  "prof.identity": { en: "Identity", ms: "Identiti", zh: "身份", ta: "அடையாளம்", ar: "الهوية" },
  "prof.security": { en: "Security", ms: "Keselamatan", zh: "安全", ta: "பாதுகாப்பு", ar: "الأمان" },
  "prof.passkeys": { en: "Passkeys", ms: "Passkey", zh: "通行密钥", ta: "கடவுச்சாவிகள்", ar: "مفاتيح المرور" },
  // Issue #310: clearer tab label describing the content (social login
  // connectors), not just the vague "Connected".
  "prof.connected": { en: "Social accounts", ms: "Akaun sosial", zh: "社交账户", ta: "சமூக கணக்குகள்", ar: "الحسابات الاجتماعية" },
  // Issue #307: user-level MCP connected agents (OAuth clients bound to the
  // user — a connected agent can act across all the user's workspaces).
  "prof.mcp": { en: "MCP agents", ms: "Agent MCP", zh: "MCP 代理", ta: "MCP முகவர்கள்", ar: "وكلاء MCP" },
  "prof.mcpTitle": { en: "Connected agents (MCP)", ms: "Agent bersambung (MCP)", zh: "已连接的代理（MCP）", ta: "இணைக்கப்பட்ட முகவர்கள் (MCP)", ar: "الوكلاء المتصلون (MCP)" },
  // issue #395: inline "?" doc link next to the MCP settings
  "prof.mcpDocs": { en: "MCP tool reference", ms: "Rujukan alat MCP", zh: "MCP 工具参考", ta: "MCP கருவி குறிப்பு", ar: "مرجع أدوات MCP" },
  "prof.mcpDisconnectConfirm": { en: "Disconnect this agent? It will lose access to every workspace it could reach", ms: "Putuskan sambungan agent ini? Ia akan hilang akses ke semua ruang kerja yang boleh dicapainya", zh: "断开此代理？它将失去对所有可达工作区的访问", ta: "இந்த முகவரைத் துண்டிக்கவா? அது அடையக்கூடிய ஒவ்வொரு பணியிடத்திற்கும் அணுகலை இழக்கும்", ar: "قطع اتصال هذا الوكيل؟ سيفقد الوصول إلى كل مساحة عمل يمكنه الوصول إليها" },
  "prof.mcpScopeTitle": { en: "Scope", ms: "Skop", zh: "范围", ta: "நோக்கம்", ar: "النطاق" },
  "prof.mcpScopeHelp": { en: "A connected agent can read, manage and administer every workspace you own, up to your role in each workspace. It is never granted more than your own role in any workspace.", ms: "Agent bersambung boleh membaca, mengurus dan mentadbir setiap ruang kerja milik anda — setakat peranan anda dalam setiap ruang kerja. Ia tidak pernah diberi lebih daripada peranan anda sendiri dalam mana-mana ruang kerja.", zh: "已连接的代理可以读取、管理和维护您拥有的每个工作区 — 不超过您在每个工作区中的角色。它在任何工作区中都绝不会获得超过您自身角色的权限。", ta: "இணைக்கப்பட்ட முகவர் நீங்கள் வைத்திருக்கும் ஒவ்வொரு பணியிடத்தையும் படிக்கவும், நிர்வகிக்கவும், நிருவகிக்கவும் முடியும் — ஒவ்வொரு பணியிடத்திலும் உங்கள் பாத்திரத்தின் அளவுக்கு. எந்த பணியிடத்திலும் அது உங்கள் சொந்த பாத்திரத்தை விட அதிகமாக ஒருபோதும் வழங்கப்படாது.", ar: "يمكن للوكيل المتصل قراءة وإدارة وتدبير كل مساحة عمل تملكها — وفقاً لدورك في كل مساحة عمل. لا يُمنح أبداً أكثر من دورك الخاص في أي مساحة عمل." },
  // issue #426: mobile-friendly MCP tab — compact technical detail rows,
  // copyable server URL box, clearer empty state with a Connect CTA.
  "prof.mcpDetailOAuth": { en: "OAuth 2.1", ms: "OAuth 2.1", zh: "OAuth 2.1", ta: "OAuth 2.1", ar: "OAuth 2.1" },
  "prof.mcpDetailDcr": { en: "Dynamic client registration", ms: "Pendaftaran klien dinamik", zh: "动态客户端注册", ta: "மாறும் க்ளையன்ட் பதிவு", ar: "تسجيل ديناميكي للعملاء" },
  "prof.mcpDetailPkce": { en: "PKCE", ms: "PKCE", zh: "PKCE", ta: "PKCE", ar: "PKCE" },
  "prof.mcpDetailNoKeys": { en: "No API keys needed", ms: "Tanpa kunci API", zh: "无需 API 密钥", ta: "API விசைகள் தேவையில்லை", ar: "لا حاجة لمفاتيح API" },
  "prof.mcpEmptyTitle": { en: "No agents connected yet", ms: "Belum ada agent bersambung", zh: "尚无代理连接", ta: "இன்னும் முகவர்கள் இணைக்கப்படவில்லை", ar: "لا يوجد وكلاء متصلون بعد" },
  "prof.mcpEmptyHelp": { en: "Point any MCP client at the server URL. It registers itself automatically.", ms: "Tunjuk mana-mana klien MCP ke URL pelayan — ia daftar sendiri secara automatik.", zh: "将任何 MCP 客户端指向服务器 URL — 它会自动注册。", ta: "எந்த MCP க்ளையன்டையும் சர்வர் URL-ஐ சுட்டிக்காட்டுங்கள் — அது தானே பதிவுசெய்யும்.", ar: "وجّه أي عميل MCP إلى عنوان URL للخادم — سيسجل نفسه تلقائياً." },
  "prof.mcpConnect": { en: "Connect an agent", ms: "Sambung agent", zh: "连接代理", ta: "முகவரை இணைக்கவும்", ar: "ربط وكيل" },
  // Card t_9a6cc85f: "Connected apps" (one row per live OAuth credential, with
  // revoke + the scope ladder) and the RFC 8628 consent card the device-flow
  // email links to.
  "prof.appsTitle": { en: "Connected apps", ms: "Apl bersambung", zh: "已连接的应用", ta: "இணைக்கப்பட்ட செயலிகள்", ar: "التطبيقات المتصلة" },
  "prof.appsDocs": { en: "What these connections can do", ms: "Apa yang boleh dilakukan sambungan ini", zh: "这些连接能做什么", ta: "இந்த இணைப்புகள் என்ன செய்ய முடியும்", ar: "ما يمكن لهذه الاتصالات فعله" },
  "prof.appsHelp": { en: "Every credential an AI agent holds for your account. Revoking one ends its access immediately, including the token it would use to refresh itself.", ms: "Setiap kelayakan yang dipegang ejen AI untuk akaun anda. Membatalkan satu menamatkan aksesnya serta-merta, termasuk token yang akan digunakannya untuk memperbaharui diri.", zh: "AI 代理为您的账户持有的每一份凭据。撤销后其访问立即终止，包括它用来刷新的令牌。", ta: "உங்கள் கணக்கிற்காக AI முகவர் வைத்திருக்கும் ஒவ்வொரு அனுமதிச் சீட்டும். ஒன்றை ரத்துசெய்தால் அதன் அணுகல் உடனடியாக முடிவடையும் — தன்னைப் புதுப்பிக்கப் பயன்படுத்தும் டோக்கன் உட்பட.", ar: "كل تصريح يحمله وكيل ذكاء اصطناعي لحسابك. إلغاء أحدها ينهي وصوله فوراً، بما في ذلك الرمز الذي يستخدمه لتجديد نفسه." },
  "prof.appsScope": { en: "Access", ms: "Akses", zh: "权限", ta: "அணுகல்", ar: "الوصول" },
  "prof.appsScopeRead": { en: "Read only", ms: "Baca sahaja", zh: "只读", ta: "படிக்க மட்டும்", ar: "للقراءة فقط" },
  "prof.appsScopeReadWrite": { en: "Read and write", ms: "Baca dan tulis", zh: "读写", ta: "படிக்க மற்றும் எழுத", ar: "قراءة وكتابة" },
  "prof.appsCreated": { en: "Created", ms: "Dicipta", zh: "创建于", ta: "உருவாக்கப்பட்டது", ar: "أُنشئ" },
  "prof.appsLastUsed": { en: "Last used", ms: "Kali terakhir digunakan", zh: "上次使用", ta: "கடைசியாகப் பயன்படுத்தப்பட்டது", ar: "آخر استخدام" },
  "prof.appsNever": { en: "never", ms: "belum", zh: "从未", ta: "இல்லை", ar: "أبداً" },
  "prof.appsRevoke": { en: "Revoke", ms: "Batalkan", zh: "撤销", ta: "ரத்துசெய்", ar: "إلغاء" },
  "prof.appsRevokeConfirm": { en: "Revoke this connection? The agent loses access immediately.", ms: "Batalkan sambungan ini? Agent kehilangan akses serta-merta.", zh: "撤销此连接？代理将立即失去访问权限。", ta: "இந்த இணைப்பை ரத்துசெய்யவா? முகவர் உடனடியாக அணுகலை இழக்கும்.", ar: "إلغاء هذا الاتصال؟ سيفقد الوكيل الوصول فوراً." },
  "prof.appsUpgradeConfirm": { en: "Give this agent write access? It will be able to create, change and delete your forms, products and sales. The current credential stops working.", ms: "Beri ejen ini akses tulis? Ia boleh mencipta, mengubah dan memadam borang, produk dan jualan anda — dan kelayakan semasa tidak lagi berfungsi.", zh: "授予此代理写入权限？它将可以创建、更改和删除您的表单、产品和销售 — 当前凭据将失效。", ta: "இந்த முகவருக்கு எழுதும் அணுகலை வழங்கவா? உங்கள் படிவங்கள், தயாரிப்புகள் மற்றும் விற்பனைகளை உருவாக்க, மாற்ற, நீக்க முடியும் — தற்போதைய அனுமதிச் சீட்டு செயலிழக்கும்.", ar: "منح هذا الوكيل صلاحية الكتابة؟ سيتمكن من إنشاء نماذجك ومنتجاتك ومبيعاتك وتغييرها وحذفها — وسيتوقف التصريح الحالي عن العمل." },
  "prof.appsRotatedTitle": { en: "New credential", ms: "Kelayakan baharu", zh: "新凭据", ta: "புதிய அனுமதிச் சீட்டு", ar: "تصريح جديد" },
  "prof.appsRotatedHelp": { en: "The previous credential no longer works. Copy these into your AI harness now: they are shown once.", ms: "Kelayakan terdahulu tidak lagi berfungsi. Salin ini ke dalam harness AI anda sekarang — ia dipaparkan sekali sahaja.", zh: "之前的凭据已失效。现在将这些复制到您的 AI 工具中 — 只显示一次。", ta: "முந்தைய அனுமதிச் சீட்டு செயலிழந்தது. இவற்றை இப்போது உங்கள் AI கருவியில் நகலெடுக்கவும் — ஒருமுறை மட்டுமே காட்டப்படும்.", ar: "التصريح السابق لم يعد يعمل. انسخ هذه الآن إلى أداتك — تُعرض مرة واحدة فقط." },
  "prof.appsEmpty": { en: "No agent has connected yet.", ms: "Belum ada agent bersambung.", zh: "尚无代理连接。", ta: "இன்னும் எந்த முகவரும் இணைக்கப்படவில்லை.", ar: "لم يتصل أي وكيل بعد." },
  "prof.deviceTitle": { en: "Approve an AI agent connection", ms: "Luluskan sambungan ejen AI", zh: "批准 AI 代理连接", ta: "AI முகவர் இணைப்பை அனுமதிக்கவும்", ar: "الموافقة على اتصال وكيل ذكاء اصطناعي" },
  "prof.deviceHelp": { en: "A harness is waiting for this code. Approving gives it access to your account. Deny it unless you started the request.", ms: "Sesebuah harness sedang menunggu kod ini. Meluluskan memberi akses ke akaun anda — tolak jika anda tidak memulakannya.", zh: "有一个工具正在等待此代码。批准将授予其访问您账户的权限 — 如果不是您发起的，请拒绝。", ta: "ஒரு கருவி இந்தக் குறியீட்டிற்காகக் காத்திருக்கிறது. அனுமதித்தால் உங்கள் கணக்கிற்கான அணுகல் வழங்கப்படும் — நீங்கள் தொடங்கவில்லை என்றால் நிராகரிக்கவும்.", ar: "أداة تنتظر هذا الرمز. الموافقة تمنحها الوصول إلى حسابك — ارفض إن لم تكن أنت من بدأ الطلب." },
  "prof.deviceClient": { en: "Client", ms: "Klien", zh: "客户端", ta: "கிளையன்ட்", ar: "العميل" },
  "prof.deviceApprove": { en: "Approve", ms: "Luluskan", zh: "批准", ta: "அனுமதி", ar: "موافقة" },
  "prof.deviceDeny": { en: "Deny", ms: "Tolak", zh: "拒绝", ta: "நிராகரி", ar: "رفض" },
  "prof.deviceApproved": { en: "Approved. Return to your AI harness: it picks up the connection on its next check.", ms: "Diluluskan. Kembali ke harness AI anda — ia akan mengambil sambungan pada pemeriksaan seterusnya.", zh: "已批准。回到您的 AI 工具 — 它会在下次检查时取得连接。", ta: "அனுமதிக்கப்பட்டது. உங்கள் AI கருவிக்குத் திரும்புங்கள் — அடுத்த சரிபார்ப்பில் இணைப்பைப் பெறும்.", ar: "تمت الموافقة. عُد إلى أداتك — ستلتقط الاتصال في فحصها التالي." },
  "prof.deviceDenied": { en: "Denied. The agent cannot connect.", ms: "Ditolak. Agent tidak boleh bersambung.", zh: "已拒绝。代理无法连接。", ta: "நிராகரிக்கப்பட்டது. முகவர் இணைக்க முடியாது.", ar: "تم الرفض. لا يمكن للوكيل الاتصال." },
  "prof.deviceEnterCode": { en: "Code from the email", ms: "Kod dari emel", zh: "邮件中的代码", ta: "மின்னஞ்சலில் உள்ள குறியீடு", ar: "الرمز من البريد" },
  "prof.deviceCheck": { en: "Check code", ms: "Semak kod", zh: "检查代码", ta: "குறியீட்டைச் சரிபார்", ar: "تحقق من الرمز" },
  "prof.deviceNotFound": { en: "This code is not valid or has expired.", ms: "Kod ini tidak sah atau sudah tamat tempoh.", zh: "此代码无效或已过期。", ta: "இந்தக் குறியீடு செல்லாதது அல்லது காலாவதியானது.", ar: "هذا الرمز غير صالح أو منتهي." },
  // Card t_cf45b84c: the access-level choice on the consent card. The visible
  // labels state the CONSEQUENCE in plain words — no "scope", no "grant" — so
  // the person can tell what they are handing over without knowing OAuth.
  "prof.deviceScopeLegend": { en: "What may this agent do?", ms: "Apa yang boleh ejen ini lakukan?", zh: "这个代理可以做什么？", ta: "இந்த முகவர் என்ன செய்யலாம்?", ar: "ماذا يمكن لهذا الوكيل أن يفعل؟" },
  "prof.deviceScopeRead": { en: "Look only", ms: "Lihat sahaja", zh: "仅查看", ta: "பார்க்க மட்டும்", ar: "عرض فقط" },
  "prof.deviceScopeReadHelp": { en: "It can see your forms, products and sales, but cannot change anything.", ms: "Ia boleh melihat borang, produk dan jualan anda, tetapi tidak boleh mengubah apa-apa.", zh: "它可以查看您的表单、产品和销售，但不能更改任何内容。", ta: "உங்கள் படிவங்கள், தயாரிப்புகள் மற்றும் விற்பனைகளைப் பார்க்கலாம், ஆனால் எதையும் மாற்ற முடியாது.", ar: "يمكنه رؤية نماذجك ومنتجاتك ومبيعاتك، لكنه لا يستطيع تغيير أي شيء." },
  "prof.deviceScopeReadWrite": { en: "Look and change", ms: "Lihat dan ubah", zh: "查看并更改", ta: "பார்க்கவும் மாற்றவும்", ar: "عرض وتغيير" },
  "prof.deviceScopeReadWriteHelp": { en: "It can also create and change your forms, products and sales.", ms: "Ia juga boleh mencipta dan mengubah borang, produk dan jualan anda.", zh: "它还可以创建和更改您的表单、产品和销售。", ta: "உங்கள் படிவங்கள், தயாரிப்புகள் மற்றும் விற்பனைகளை உருவாக்கவும் மாற்றவும் முடியும்.", ar: "يمكنه أيضاً إنشاء نماذجك ومنتجاتك ومبيعاتك وتغييرها." },
  "prof.connectedTitle": { en: "Connected accounts", ms: "Akaun bersambung", zh: "已连接的账户", ta: "இணைக்கப்பட்ட கணக்குகள்", ar: "الحسابات المتصلة" },
  "prof.connectedHelp": { en: "Link a Google account to sign in with one click. More providers (Microsoft/Azure, GitHub, Cloudflare) can be enabled later through the same connector system. These connections belong to you, not to any single workspace.", ms: "Paut akaun Google untuk log masuk satu klik. Pembekal lain (Microsoft/Azure, GitHub, Cloudflare) boleh diaktifkan kemudian — sistem penyambung sama. Sambungan ini milik anda, bukan mana-mana ruang kerja.", zh: "关联 Google 账户即可一键登录。之后可启用更多提供商（Microsoft/Azure、GitHub、Cloudflare）— 同一连接器系统。这些连接属于您本人，而非任何单个工作区。", ta: "Google கணக்கை இணைத்து, ஒரே கிளிக்கில் உள்நுழையவும். பிற வழங்குநர்கள் (Microsoft/Azure, GitHub, Cloudflare) பின்னர் இயக்கக்கூடியவை — அதே இணைப்பான் அமைப்பு. இந்த இணைப்புகள் எந்த ஒற்றை பணியிடத்தின் சொந்தம் அல்ல, உங்களுடையவை.", ar: "اربط حساب Google لتسجيل الدخول بنقرة واحدة. يمكن تفعيل مزودين آخرين (Microsoft/Azure وGitHub وCloudflare) لاحقاً — بنفس نظام الربط. هذه الاتصالات ملك لك، وليست لأي مساحة عمل معينة." },
  "prof.linked": { en: "linked", ms: "dipaut", zh: "已关联", ta: "இணைக்கப்பட்டது", ar: "مرتبط" },
  "prof.unlink": { en: "Unlink", ms: "Nyahpaut", zh: "取消关联", ta: "இணைப்பை நீக்கு", ar: "إلغاء الربط" },
  "prof.noLinked": { en: "No linked accounts yet.", ms: "Belum ada akaun dipaut.", zh: "尚未关联任何账户。", ta: "இன்னும் இணைக்கப்பட்ட கணக்குகள் இல்லை.", ar: "لا حسابات مرتبطة بعد." },
  "prof.linkGoogle": { en: "Link Google account", ms: "Paut akaun Google", zh: "关联 Google 账户", ta: "Google கணக்கை இணைக்கவும்", ar: "ربط حساب Google" },
  "prof.displayName": { en: "Display name", ms: "Nama paparan", zh: "显示名称", ta: "காட்சி பெயர்", ar: "اسم العرض" },
  "prof.yourName": { en: "Your name", ms: "Nama anda", zh: "您的姓名", ta: "உங்கள் பெயர்", ar: "اسمك" },
  "prof.saveName": { en: "Save name", ms: "Simpan nama", zh: "保存姓名", ta: "பெயரைச் சேமி", ar: "حفظ الاسم" },
  // issue #181: optional phone number
  "prof.phone": { en: "Phone number (optional)", ms: "Nombor telefon (pilihan)", zh: "电话号码（可选）", ta: "தொலைபேசி எண் (விருப்பம்)", ar: "رقم الهاتف (اختياري)" },
  "prof.phonePlaceholder": { en: "+60 12 345 6789", ms: "+60 12 345 6789", zh: "+60 12 345 6789", ta: "+60 12 345 6789", ar: "+60 12 345 6789" },
  "prof.savePhone": { en: "Save phone", ms: "Simpan nombor", zh: "保存电话号码", ta: "எண்ணைச் சேமி", ar: "حفظ الرقم" },
  // issue #191: preferred timezone
  "prof.timezone": { en: "Timezone", ms: "Zona waktu", zh: "时区", ta: "நேர மண்டலம்", ar: "المنطقة الزمنية" },
  "prof.timezoneAuto": { en: "Auto (follow browser/PC)", ms: "Auto (ikut pelayar/PC)", zh: "自动（跟随浏览器/电脑）", ta: "தானியங்கி (உலாவி/கணினியைப் பின்தொடர்)", ar: "تلقائي (اتبع المتصفح/الكمبيوتر)" },
  "prof.saveTimezone": { en: "Save timezone", ms: "Simpan zona waktu", zh: "保存时区", ta: "நேர மண்டலத்தைச் சேமி", ar: "حفظ المنطقة الزمنية" },
  // Language placement: the dashboard UI language is PER-PROFILE (it follows the
  // person into every browser). This is the dashboard's language only — the
  // language of a PUBLIC form is set per form / per workspace in Settings.
  "prof.language": { en: "Dashboard language", ms: "Bahasa dashboard", zh: "仪表板语言", ta: "டாஷ்போர்டு மொழி", ar: "لغة لوحة التحكم" },
  "prof.languageHelp": { en: "The language this dashboard is shown in, for your account only. It follows you into any browser you sign in from. It does not change the language your customers see on a public form; that is set per form or per workspace in Settings.", ms: "Bahasa yang digunakan untuk dashboard ini, untuk akaun anda sahaja. Ia mengikut anda ke mana-mana pelayar anda log masuk. Ia tidak mengubah bahasa yang pelanggan anda lihat pada borang awam — itu ditetapkan per borang atau per workspace dalam Tetapan.", zh: "此仪表板显示的语言，仅适用于您的账户。无论您从哪个浏览器登录都会沿用。它不会改变客户在公开表单上看到的语言 — 那是在设置中按表单或按工作区设置的。", ta: "இந்த டாஷ்போர்டு காட்டப்படும் மொழி, உங்கள் கணக்கிற்கு மட்டும். நீங்கள் உள்நுழையும் எந்த உலாவிக்கும் இது தொடரும். உங்கள் வாடிக்கையாளர்கள் பொது படிவத்தில் காணும் மொழியை இது மாற்றாது — அது அமைப்புகளில் படிவம் அல்லது பணியிடம் வாரியாக அமைக்கப்படுகிறது.", ar: "اللغة التي تُعرض بها لوحة التحكم هذه، لحسابك فقط. تتبعك إلى أي متصفح تسجّل الدخول منه، ولا تغيّر اللغة التي يراها عملاؤك في نموذج عام — تلك تُحدَّد لكل نموذج أو لكل مساحة عمل في الإعدادات." },
  "prof.languageSaved": { en: "Language saved ✓", ms: "Bahasa disimpan ✓", zh: "语言已保存 ✓", ta: "மொழி சேமிக்கப்பட்டது ✓", ar: "تم حفظ اللغة ✓" },
  "prof.languageDocs": { en: "How dashboard and form language differ", ms: "Beza bahasa dashboard dan bahasa borang", zh: "仪表板语言与表单语言的区别", ta: "டாஷ்போர்டு மொழிக்கும் படிவ மொழிக்கும் உள்ள வேறுபாடு", ar: "الفرق بين لغة لوحة التحكم ولغة النموذج" },
  "prof.emailAddress": { en: "Email address", ms: "Alamat emel", zh: "电子邮件地址", ta: "மின்னஞ்சல் முகவரி", ar: "البريد الإلكتروني" },
  "prof.current": { en: "Current:", ms: "Semasa:", zh: "当前：", ta: "தற்போதைய:", ar: "الحالي:" },
  "prof.newEmail": { en: "New email", ms: "Emel baharu", zh: "新邮箱", ta: "புதிய மின்னஞ்சல்", ar: "بريد إلكتروني جديد" },
  "prof.currentPwConfirm": { en: "Current password (to confirm)", ms: "Kata laluan semasa (untuk sahkan)", zh: "当前密码（用于确认）", ta: "தற்போதைய கடவுச்சொல் (உறுதிப்படுத்த)", ar: "كلمة المرور الحالية (للتأكيد)" },
  "prof.changeEmail": { en: "Change email", ms: "Tukar emel", zh: "更改邮箱", ta: "மின்னஞ்சலை மாற்று", ar: "تغيير البريد الإلكتروني" },
  "prof.password": { en: "Password", ms: "Kata laluan", zh: "密码", ta: "கடவுச்சொல்", ar: "كلمة المرور" },
  "prof.currentPw": { en: "Current password", ms: "Kata laluan semasa", zh: "当前密码", ta: "தற்போதைய கடவுச்சொல்", ar: "كلمة المرور الحالية" },
  "prof.newPw": { en: "New password (min. 10 chars)", ms: "Kata laluan baharu (min. 10 aksara)", zh: "新密码（至少 10 个字符）", ta: "புதிய கடவுச்சொல் (குறைந்தது 10 எழுத்துகள்)", ar: "كلمة المرور الجديدة (10 أحرف على الأقل)" },
  "prof.confirmPw": { en: "Confirm new password", ms: "Sahkan kata laluan baharu", zh: "确认新密码", ta: "புதிய கடவுச்சொல்லை உறுதிப்படுத்து", ar: "تأكيد كلمة المرور الجديدة" },
  "prof.changePw": { en: "Change password", ms: "Tukar kata laluan", zh: "更改密码", ta: "கடவுச்சொல்லை மாற்று", ar: "تغيير كلمة المرور" },
  "prof.pwMismatch": { en: "Passwords don't match", ms: "Kata laluan tidak sepadan", zh: "两次输入的密码不一致", ta: "கடவுச்சொற்கள் பொருந்தவில்லை", ar: "كلمتا المرور غير متطابقتين" },
  "prof.twoFa": { en: "Two-factor authentication (TOTP)", ms: "Pengesahan dua faktor (TOTP)", zh: "双因素认证（TOTP）", ta: "இரு-காரணி அங்கீகாரம் (TOTP)", ar: "المصادقة الثنائية (TOTP)" },
  "prof.status": { en: "Status:", ms: "Status:", zh: "状态：", ta: "நிலை:", ar: "الحالة:" },
  "prof.enabled": { en: "Enabled ✓", ms: "Aktif ✓", zh: "已启用 ✓", ta: "இயக்கப்பட்டது ✓", ar: "مُمكّن ✓" },
  "prof.notEnabled": { en: "Not enabled", ms: "Belum aktif", zh: "未启用", ta: "இயக்கப்படவில்லை", ar: "غير مُمكّن" },
  "prof.setup2fa": { en: "Set up 2FA", ms: "Sediakan 2FA", zh: "设置 2FA", ta: "2FA அமை", ar: "إعداد التحقق الثنائي" },
  "prof.resetup2fa": { en: "Re-setup 2FA", ms: "Sediakan semula 2FA", zh: "重新设置 2FA", ta: "2FA மீண்டும் அமை", ar: "إعادة إعداد التحقق الثنائي" },
  "prof.disable2fa": { en: "Disable 2FA", ms: "Nyahaktif 2FA", zh: "停用 2FA", ta: "2FA முடக்கு", ar: "تعطيل التحقق الثنائي" },
  "prof.enter2faCode": { en: "Enter a current 2FA code to disable:", ms: "Masukkan kod 2FA semasa untuk nyahaktif:", zh: "输入当前的 2FA 验证码以停用：", ta: "2FA ஐ முடக்க தற்போதைய குறியீட்டை உள்ளிடவும்:", ar: "أدخل رمز التحقق الثنائي الحالي للتعطيل:" },
  "prof.twoFaDocs": { en: "How 2FA and disabling it work", ms: "Cara 2FA dan nyahaktifnya berfungsi", zh: "2FA 及其停用方式", ta: "2FA மற்றும் அதை முடக்குவது எப்படி", ar: "كيف يعمل التحقق الثنائي وتعطيله" },
  // Issue #458: a magic-link session for a 2FA user must confirm a fresh TOTP
  // code before a sensitive action (change password / disable 2FA / add
  // passkey). Only those users see this prompt.
  "prof.stepUpTitle": { en: "Confirm your 2FA code", ms: "Sahkan kod 2FA anda", zh: "确认您的 2FA 验证码", ta: "உங்கள் 2FA குறியீட்டை உறுதிப்படுத்தவும்", ar: "أكّد رمز التحقق الثنائي" },
  "prof.stepUpDesc": { en: "You signed in with an email link. Enter a current 2FA code to continue.", ms: "Anda log masuk melalui pautan emel. Masukkan kod 2FA semasa untuk teruskan.", zh: "您通过电子邮件链接登录。请输入当前的 2FA 验证码以继续。", ta: "நீங்கள் மின்னஞ்சல் இணைப்பு மூலம் உள்நுழைந்தீர்கள். தொடர தற்போதைய 2FA குறியீட்டை உள்ளிடவும்.", ar: "لقد سجّلت الدخول عبر رابط البريد الإلكتروني. أدخل رمز التحقق الثنائي الحالي للمتابعة." },
  "prof.stepUpConfirm": { en: "Confirm", ms: "Sahkan", zh: "确认", ta: "உறுதிப்படுத்து", ar: "تأكيد" },
  "prof.stepUpInvalid": { en: "Enter the current 6-digit code from your authenticator app.", ms: "Masukkan kod 6 digit semasa daripada aplikasi pengesah anda.", zh: "请输入验证器应用中的当前 6 位验证码。", ta: "உங்கள் அங்கீகாரப் பயன்பாட்டிலிருந்து தற்போதைய 6 இலக்கக் குறியீட்டை உள்ளிடவும்.", ar: "أدخل الرمز الحالي المكوّن من 6 أرقام من تطبيق المصادقة." },
  "prof.twoFaSetupTitle": { en: "Set up two-factor authentication", ms: "Sediakan pengesahan dua faktor", zh: "设置双因素认证", ta: "இரு-காரணி அங்கீகாரத்தை அமை", ar: "إعداد المصادقة الثنائية" },
  // Alt text for the enrolment QR image (card t_b432c9ca): a screen reader
  // announces it, so it is translated like the steps around it.
  "prof.qrAlt": { en: "QR code", ms: "Kod QR", zh: "二维码", ta: "QR குறியீடு", ar: "رمز QR" },
  "prof.twoFaScan": { en: "1. Scan the QR code with your authenticator app", ms: "1. Imbas kod QR dengan aplikasi autentikator anda", zh: "1. 使用您的验证器应用扫描二维码", ta: "1. உங்கள் அங்கீகாரப் பயன்பாட்டில் QR குறியீட்டை ஸ்கேன் செய்யவும்", ar: "1. امسح رمز QR بتطبيق المصادقة الخاص بك" },
  "prof.twoFaManual": { en: "2. Or enter this code manually", ms: "2. Atau masukkan kod ini secara manual", zh: "2. 或手动输入此代码", ta: "2. அல்லது இந்தக் குறியீட்டை கைமுறையாக உள்ளிடவும்", ar: "2. أو أدخل هذا الرمز يدويًا" },
  "prof.twoFaCodeLabel": { en: "3. Enter the 6-digit code from your app", ms: "3. Masukkan kod 6 digit daripada aplikasi anda", zh: "3. 输入应用中的 6 位代码", ta: "3. உங்கள் பயன்பாட்டிலிருந்து 6-இலக்க குறியீட்டை உள்ளிடவும்", ar: "3. أدخل الرمز المكوّن من 6 أرقام من تطبيقك" },
  "prof.twoFaCodePlaceholder": { en: "000000", ms: "000000", zh: "000000", ta: "000000", ar: "000000" },
  "prof.twoFaEnabling": { en: "Enable 2FA", ms: "Aktifkan 2FA", zh: "启用 2FA", ta: "2FA செயல்படுத்து", ar: "تفعيل التحقق الثنائي" },
  "prof.twoFaEnabled": { en: "2FA enabled ✓", ms: "2FA diaktifkan ✓", zh: "2FA 已启用 ✓", ta: "2FA செயல்படுத்தப்பட்டது ✓", ar: "تم تفعيل التحقق الثنائي ✓" },
  "prof.twoFaClose": { en: "Cancel", ms: "Batal", zh: "取消", ta: "ரத்து", ar: "إلغاء" },
  "prof.twoFaSetupError": { en: "Could not start 2FA setup. Please try again.", ms: "Tidak dapat memulakan penyediaan 2FA. Sila cuba lagi.", zh: "无法开始 2FA 设置。请重试。", ta: "2FA அமைப்பைத் தொடங்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذر بدء إعداد التحقق الثنائي. يرجى المحاولة مرة أخرى." },
  "prof.passkeyDesc": { en: "Sign in with your fingerprint, face or device PIN. No password.", ms: "Log masuk dengan cap jari / wajah / PIN peranti — tanpa kata laluan.", zh: "使用指纹 / 面容 / 设备 PIN 码登录 — 无需密码。", ta: "கைரேகை / முகம் / சாதன PIN மூலம் உள்நுழையவும் — கடவுச்சொல் தேவையில்லை.", ar: "سجّل الدخول ببصمة الإصبع / الوجه / رمز PIN للجهاز — بدون كلمة مرور." },
  "prof.registerPasskey": { en: "Register passkey", ms: "Daftar passkey", zh: "注册通行密钥", ta: "கடவுச்சாவியை பதிவுசெய்", ar: "تسجيل مفتاح المرور" },
  // t_254d8a81: the inline "?" doc link on the Profile → Passkeys control
  // (AGENTS.md docs-parity rule 4), matching prof.twoFaDocs on the Security tab.
  // web/src/lib/passkeyDocs.test.ts pins the link this title belongs to.
  "prof.passkeysDocs": { en: "How passkeys sign you in", ms: "Cara passkey log masuk untuk anda", zh: "通行密钥如何登录", ta: "கடவுச்சாவிகள் உங்களை எவ்வாறு உள்நுழைக்கின்றன", ar: "كيف تسجّل مفاتيح المرور دخولك" },
  "prof.removePasskeyConfirm": { en: "Remove this passkey?", ms: "Buang passkey ini?", zh: "移除此通行密钥？", ta: "இந்த கடவுச்சாவியை நீக்கவா?", ar: "إزالة مفتاح المرور هذا؟" },
  "prof.remove": { en: "Remove", ms: "Buang", zh: "移除", ta: "நீக்கு", ar: "إزالة" },
  "prof.saved": { en: "Saved ✓", ms: "Disimpan ✓", zh: "已保存 ✓", ta: "சேமிக்கப்பட்டது ✓", ar: "تم الحفظ ✓" },
  "prof.failed": { en: "The change was not saved. Try again.", ms: "Perubahan tidak disimpan. Cuba semula.", zh: "更改未保存。请重试。", ta: "மாற்றம் சேமிக்கப்படவில்லை. மீண்டும் முயலவும்.", ar: "لم يتم حفظ التغيير. أعد المحاولة." },
  // issue #133: profile picture upload
  "prof.profilePicture": { en: "Profile picture", ms: "Gambar profil", zh: "个人资料照片", ta: "சுயவிவரப் படம்", ar: "صورة الملف الشخصي" },
  "prof.uploadAvatar": { en: "Upload picture", ms: "Muat naik gambar", zh: "上传照片", ta: "படத்தைப் பதிவேற்று", ar: "رفع صورة" },
  "prof.uploadingAvatar": { en: "Uploading…", ms: "Memuat naik…", zh: "上传中…", ta: "பதிவேற்றுகிறது…", ar: "جارٍ الرفع…" },
  "prof.removeAvatar": { en: "Remove", ms: "Buang", zh: "移除", ta: "அகற்று", ar: "إزالة" },
  "prof.avatarSaved": { en: "Profile picture updated ✓", ms: "Gambar profil dikemas kini ✓", zh: "个人资料照片已更新 ✓", ta: "சுயவிவரப் படம் புதுப்பிக்கப்பட்டது ✓", ar: "تم تحديث صورة الملف الشخصي ✓" },
  "prof.avatarError": { en: "Failed to upload picture.", ms: "Gagal muat naik gambar.", zh: "上传照片失败。", ta: "படத்தைப் பதிவேற்ற முடியவில்லை.", ar: "فشل رفع الصورة." },
  "prof.avatarTypeError": { en: "Please choose a PNG, JPG, WebP or GIF image.", ms: "Sila pilih imej PNG, JPG, WebP atau GIF.", zh: "请选择 PNG、JPG、WebP 或 GIF 图片。", ta: "PNG, JPG, WebP அல்லது GIF படத்தைத் தேர்ந்தெடுக்கவும்.", ar: "يرجى اختيار صورة PNG أو JPG أو WebP أو GIF." },
  "prof.passkeyRegistered": { en: "Passkey registered ✓", ms: "Passkey berdaftar ✓", zh: "通行密钥已注册 ✓", ta: "கடவுச்சாவி பதிவுசெய்யப்பட்டது ✓", ar: "تم تسجيل مفتاح المرور ✓" },
  "prof.passkeyRegFailed": { en: "Failed to register passkey", ms: "Gagal daftar passkey", zh: "注册通行密钥失败", ta: "கடவுச்சாவி பதிவு தோல்வி", ar: "فشل تسجيل مفتاح المرور" },
  "prof.passkeyUnsupported": { en: "Passkey cancelled or not supported by this browser.", ms: "Passkey dibatalkan atau tidak disokong pelayar ini.", zh: "通行密钥已取消或此浏览器不支持。", ta: "கடவுச்சாவி ரத்துசெய்யப்பட்டது அல்லது இந்த உலாவி ஆதரிக்கவில்லை.", ar: "تم إلغاء مفتاح المرور أو المتصفح لا يدعمه." },
  // builder
  "bld.formTitle": { en: "Form title", ms: "Tajuk borang", zh: "表单标题", ta: "படிவத் தலைப்பு", ar: "عنوان النموذج" },
  // The builder toolbar's slug input is announced from this (card t_becb0f50):
  // it has no room for a visible label, and a placeholder is not one.
  "bld.formSlug": { en: "Form URL slug", ms: "Slug URL borang", zh: "表单 URL slug", ta: "படிவ URL slug", ar: "slug رابط النموذج" },
  // issue #256: AI form builder (prompt → generated form config)
  "bld.aiTitle": { en: "AI build", ms: "Bina AI", zh: "AI 构建", ta: "AI உருவாக்கு", ar: "بناء بالذكاء الاصطناعي" },
  // The prompt textarea's accessible name, distinct from the example placeholder
  // below it (card t_becb0f50): the placeholder vanishes on input, this does not.
  "bld.aiPromptLabel": { en: "Describe the form you want", ms: "Terangkan borang yang anda mahu", zh: "描述您想要的表单", ta: "நீங்கள் விரும்பும் படிவத்தை விவரிக்கவும்", ar: "صف النموذج الذي تريده" },
  "bld.aiHint": { en: "Describe the form you want and AI will draft it for you to review.", ms: "Terangkan borang yang anda mahu dan AI akan sediakan draf untuk anda semak.", zh: "描述您想要的表单，AI 将为您起草以供审阅。", ta: "நீங்கள் விரும்பும் படிவத்தை விவரிக்கவும், AI அதை மதிப்பாய்வு செய்ய வரைவு செய்யும்.", ar: "صف النموذج الذي تريده وسيقوم الذكاء الاصطناعي بإعداد مسودة لمراجعتها." },
  "bld.aiPromptPh": { en: "e.g. a booking form for a hair salon with name, phone, date, service dropdown", ms: "cth: borang tempahan salon rambut dengan nama, telefon, tarikh, dropdown perkhidmatan", zh: "例如：美发沙龙预约表单，包含姓名、电话、日期、服务下拉框", ta: "எ.கா: பெயர், தொலைபேசி, தேதி, சேவை கீழ்தோன்றல் கொண்ட ஹேர் சலூன் பதிவு படிவம்", ar: "مثال: نموذج حجز لصالون شعر مع الاسم والهاتف والتاريخ وقائمة الخدمات" },
  "bld.aiGenerate": { en: "Generate", ms: "Jana", zh: "生成", ta: "உருவாக்கு", ar: "توليد" },
  "bld.aiGenerating": { en: "Generating…", ms: "Menjana…", zh: "生成中…", ta: "உருவாக்குகிறது…", ar: "جارٍ التوليد…" },
  "bld.aiError": { en: "AI generation failed: {error}", ms: "Penjanaan AI gagal: {error}", zh: "AI 生成失败：{error}", ta: "AI உருவாக்கம் தோல்வி: {error}", ar: "فشل توليد الذكاء الاصطناعي: {error}" },
  "bld.aiApplied": { en: "AI draft applied. Review and edit before saving.", ms: "Draf AI digunakan — semak dan edit sebelum simpan.", zh: "AI 草稿已应用 — 保存前请审阅和编辑。", ta: "AI வரைவு பயன்படுத்தப்பட்டது — சேமிப்பதற்கு முன் மதிப்பாய்வு செய்து திருத்தவும்.", ar: "تم تطبيق مسودة الذكاء الاصطناعي — راجع وحرر قبل الحفظ." },
  // issue #176: form status (draft | private | published) + access password
  "bld.status": { en: "Status", ms: "Status", zh: "状态", ta: "நிலை", ar: "الحالة" },
  "bld.statusDraft": { en: "Draft", ms: "Draft", zh: "草稿", ta: "வரைவு", ar: "مسودة" },
  "bld.statusPrivate": { en: "Private", ms: "Private", zh: "私密", ta: "தனிப்பட்ட", ar: "خاص" },
  "bld.statusPublished": { en: "Published", ms: "Published", zh: "已发布", ta: "வெளியிடப்பட்டது", ar: "منشور" },
  "bld.statusUnlisted": { en: "Unlisted", ms: "Tidak disenaraikan", zh: "未列出", ta: "பட்டியலிடப்படாதது", ar: "غير مدرج" },
  "bld.statusDraftHint": { en: "Only you can see this form. Publish it to go live.", ms: "Hanya anda boleh lihat borang ini. Publish untuk live.", zh: "只有您能看到此表单。发布后即可上线。", ta: "இந்த படிவத்தை நீங்கள் மட்டுமே பார்க்கலாம். வெளியிடவும்.", ar: "أنت فقط ترى هذا النموذج. انشره ليصبح متاحاً." },
  "bld.statusPrivateHint": { en: "Password-protected. Buyers must enter the password to view it.", ms: "Dilindungi kata laluan — pembeli perlu masukkan kata laluan untuk melihat.", zh: "受密码保护 — 买家必须输入密码才能查看。", ta: "கடவுச்சொல் பாதுகாக்கப்பட்டது — வாங்குபவர்கள் பார்க்க கடவுச்சொல்லை உள்ளிட வேண்டும்.", ar: "محمي بكلمة مرور — يجب على المشترين إدخال كلمة المرور لعرضه." },
  "bld.statusPublishedHint": { en: "Live and publicly accessible.", ms: "Live dan boleh diakses awam.", zh: "已上线，可公开访问。", ta: "நேரடி மற்றும் பொதுவாக அணுகக்கூடியது.", ar: "منشور ويمكن الوصول إليه علناً." },
  "bld.statusUnlistedHint": { en: "Live via direct link but hidden from the storefront listing.", ms: "Live melalui pautan terus tetapi tersembunyi daripada senarai storefront.", zh: "可通过直接链接访问，但不在商店前台列表中显示。", ta: "நேரடி இணைப்பு மூலம் நேரடியாக உள்ளது, ஆனால் கடை முகப்பு பட்டியலில் மறைக்கப்பட்டுள்ளது.", ar: "متاح عبر الرابط المباشر لكنه مخفي من قائمة المتجر." },
  "bld.accessPassword": { en: "Access password", ms: "Kata laluan akses", zh: "访问密码", ta: "அணுகல் கடவுச்சொல்", ar: "كلمة مرور الوصول" },
  "bld.accessPasswordPh": { en: "Set a password (min. 4 chars)", ms: "Tetapkan kata laluan (min. 4 aksara)", zh: "设置密码（至少 4 个字符）", ta: "கடவுச்சொல்லை அமை (குறைந்தது 4 எழுத்துகள்)", ar: "عيّن كلمة مرور (4 أحرف على الأقل)" },
  "bld.accessPasswordHint": { en: "Buyers enter this to unlock the form. Saved when you save.", ms: "Pembeli masukkan ini untuk buka kunci borang. Disimpan bila anda simpan.", zh: "买家输入此密码以解锁表单。保存时生效。", ta: "படிவத்தைத் திறக்க வாங்குபவர்கள் இதை உள்ளிடுகின்றனர். சேமிக்கும்போது சேமிக்கப்படும்.", ar: "يدخل المشترون هذا لفتح النموذج. يُحفظ عند الحفظ." },
  "bld.view": { en: "View ↗", ms: "Lihat ↗", zh: "查看 ↗", ta: "பார்க்க ↗", ar: "عرض ↗" },
  // Onboarding gap: the "your form is live" moment after a first publish, and
  // the warning that stops a payment form with nothing to sell from going live
  // silently. The API's 409 carries code=no_products (dbstore.PublishWarningNoProducts);
  // the message below is the human-readable fallback when it is absent.
  "bld.liveTitle": { en: "Your form is live", ms: "Borang anda sudah live", zh: "您的表单已上线", ta: "உங்கள் படிவம் நேரலையில் உள்ளது", ar: "نموذجك منشور الآن" },
  "bld.liveHint": { en: "Anyone who opens this link can use your form. Share it, or embed it on your website.", ms: "Sesiapa yang membuka pautan ini boleh guna borang anda. Kongsi, atau embed pada laman web anda.", zh: "任何打开此链接的人都可以使用您的表单。分享它，或嵌入到您的网站。", ta: "இந்த இணைப்பைத் திறக்கும் எவரும் உங்கள் படிவத்தைப் பயன்படுத்தலாம். பகிரவும் அல்லது உங்கள் இணையதளத்தில் இணைக்கவும்.", ar: "أي شخص يفتح هذا الرابط يمكنه استخدام نموذجك. شاركه أو ضمّنه في موقعك." },
  "bld.openForm": { en: "Open form", ms: "Buka borang", zh: "打开表单", ta: "படிவத்தைத் திற", ar: "فتح النموذج" },
  "bld.copyUrl": { en: "Copy link", ms: "Salin pautan", zh: "复制链接", ta: "இணைப்பை நகலெடு", ar: "نسخ الرابط" },
  "bld.urlCopied": { en: "Link copied ✓", ms: "Pautan disalin ✓", zh: "链接已复制 ✓", ta: "இணைப்பு நகலெடுக்கப்பட்டது ✓", ar: "تم نسخ الرابط ✓" },
  "bld.liveNoUrl": { en: "The form is live, but its public URL is not available yet. Open Forms to find it, or set a workspace domain in Settings.", ms: "Borang sudah live, tetapi URL awamnya belum tersedia. Buka Borang untuk mencarinya, atau tetapkan domain workspace dalam Tetapan.", zh: "表单已上线，但其公开链接尚不可用。请打开“表单”查找，或在设置中设置工作区域名。", ta: "படிவம் நேரலையில் உள்ளது, ஆனால் அதன் பொதுவான URL இன்னும் கிடைக்கவில்லை. கண்டுபிடிக்க படிவங்களைத் திறக்கவும் அல்லது அமைப்புகளில் பணியிட டொமைனை அமைக்கவும்.", ar: "النموذج منشور، لكن رابطه العام غير متاح بعد. افتح النماذج للعثور عليه، أو عيّن نطاق مساحة العمل في الإعدادات." },
  "bld.dismiss": { en: "Close", ms: "Tutup", zh: "关闭", ta: "மூடு", ar: "إغلاق" },
  // t_39906c0a: the sentence must not offer a way to "collect details" — a
  // payment form with nothing to sell collects NOTHING (the legacy and block
  // payment layouts both gate submit on a selected product, and a published
  // empty payment form serves the "not accepting orders" page), so the old
  // "publish it anyway if you only meant to collect details" was a promise the
  // platform has never kept. It now says what the buyer gets instead. Guarded by
  // "the no-products refusal does not offer to collect anything" in
  // web/src/lib/publishResult.test.ts.
  "bld.noProductsWarn": { en: "This is a payment form with no products, so it cannot take money yet. It collects nothing either: a buyer cannot submit it. Add a product first, or publish it anyway if you are still setting up the catalogue.", ms: "Ini borang bayaran tanpa produk, jadi ia belum boleh menerima bayaran — dan ia tidak mengumpul apa-apa: pembeli tidak boleh menghantarnya. Tambah produk dahulu, atau teruskan publish jika katalog masih disediakan.", zh: "这是没有商品的付款表单，因此暂时无法收款，也不会收集任何内容：买家无法提交。请先添加商品，或仅在商品目录仍在设置时才照常发布。", ta: "இது தயாரிப்புகள் இல்லாத கட்டணப் படிவம், எனவே இன்னும் பணம் பெற முடியாது — எதுவும் சேகரிக்கப்படாது: வாங்குபவர் அதைச் சமர்ப்பிக்க முடியாது. முதலில் ஒரு தயாரிப்பைச் சேர்க்கவும், அல்லது பட்டியல் இன்னும் தயாராகிக் கொண்டிருக்கும்போது அப்படியே வெளியிடவும்.", ar: "هذا نموذج دفع بلا منتجات، لذا لا يمكنه تحصيل المدفوعات بعد — ولن يُجمع أي شيء: لا يمكن للمشتري إرساله. أضف منتجاً أولاً، أو انشره بينما لا يزال الكتالوج قيد الإعداد." },
  "bld.publishAnyway": { en: "Publish anyway", ms: "Publish juga", zh: "仍然发布", ta: "இருந்தாலும் வெளியிடு", ar: "نشر على أي حال" },
  // Card t_983425e5: the merchant picked a product BLOCK but no product in it,
  // so the buyer page says the form is not accepting orders. The quote is the
  // whole point — it is what the merchant never got to see.
  "bld.noProductSelectedWarn": { en: "A product block has no product selected, so buyers see \u201cThis form is not accepting orders right now.\u201d Pick a product in the block, or publish anyway.", ms: "Blok produk tiada produk dipilih, jadi pembeli melihat \u201cBorang ini tidak menerima pesanan buat masa ini.\u201d Pilih produk dalam blok itu, atau teruskan publish.", zh: "\u67d0\u4e2a\u5546\u54c1\u5757\u5c1a\u672a\u9009\u62e9\u5546\u54c1\uff0c\u56e0\u6b64\u4e70\u5bb6\u4f1a\u770b\u5230\u201c\u6b64\u8868\u5355\u76ee\u524d\u4e0d\u63a5\u53d7\u8ba2\u5355\u3002\u201d\u8bf7\u5728\u8be5\u5757\u4e2d\u9009\u62e9\u5546\u54c1\uff0c\u6216\u4ecd\u7136\u53d1\u5e03\u3002", ta: "\u0b92\u0bb0\u0bc1 \u0baa\u0bca\u0bb0\u0bc1\u0bb3\u0bcd \u0ba4\u0bca\u0b95\u0bc1\u0ba4\u0bbf\u0baf\u0bbf\u0bb2\u0bcd \u0baa\u0bca\u0bb0\u0bc1\u0bb3\u0bcd \u0ba4\u0bc7\u0bb0\u0bcd\u0ba8\u0bcd\u0ba4\u0bc6\u0b9f\u0bc1\u0b95\u0bcd\u0b95\u0baa\u0bcd\u0baa\u0b9f\u0bb5\u0bbf\u0bb2\u0bcd\u0bb2\u0bc8, \u0b8e\u0ba9\u0bb5\u0bc7 \u0bb5\u0bbe\u0b99\u0bcd\u0b95\u0bc1\u0baa\u0bb5\u0bb0\u0bcd\u0b95\u0bb3\u0bcd \u201c\u0b87\u0ba8\u0bcd\u0ba4\u0baa\u0bcd \u0baa\u0b9f\u0bbf\u0bb5\u0bae\u0bcd \u0b87\u0baa\u0bcd\u0baa\u0bcb\u0ba4\u0bc1 \u0b86\u0bb0\u0bcd\u0b9f\u0bb0\u0bcd\u0b95\u0bb3\u0bc8 \u0b8f\u0bb1\u0bcd\u0b95\u0bb5\u0bbf\u0bb2\u0bcd\u0bb2\u0bc8.\u201d \u0b8e\u0ba9\u0bcd\u0bb1\u0bc1 \u0b95\u0bbe\u0ba3\u0bcd\u0baa\u0bbe\u0bb0\u0bcd\u0b95\u0bcd\u0b95\u0bbf\u0bb1\u0bbe\u0bb0\u0bcd\u0b95\u0bb3\u0bcd. \u0ba4\u0bca\u0b95\u0bc1\u0ba4\u0bbf\u0baf\u0bbf\u0bb2\u0bcd \u0b92\u0bb0\u0bc1 \u0baa\u0bca\u0bb0\u0bc1\u0bb3\u0bc8\u0ba4\u0bcd \u0ba4\u0bc7\u0bb0\u0bcd\u0ba8\u0bcd\u0ba4\u0bc6\u0b9f\u0bc1\u0b95\u0bcd\u0b95\u0bb5\u0bc1\u0bae\u0bcd, \u0b85\u0bb2\u0bcd\u0bb2\u0ba4\u0bc1 \u0b85\u0baa\u0bcd\u0baa\u0b9f\u0bbf\u0baf\u0bc7 \u0bb5\u0bc6\u0bb3\u0bbf\u0baf\u0bbf\u0b9f\u0bb5\u0bc1\u0bae\u0bcd.", ar: "\u0643\u062a\u0644\u0629 \u0645\u0646\u062a\u062c \u0628\u062f\u0648\u0646 \u0645\u0646\u062a\u062c \u0645\u062d\u062f\u062f\u060c \u0644\u0630\u0627 \u064a\u0631\u0649 \u0627\u0644\u0645\u0634\u062a\u0631\u064a \u00ab\u0647\u0630\u0627 \u0627\u0644\u0646\u0645\u0648\u0630\u062c \u0644\u0627 \u064a\u0633\u062a\u0642\u0628\u0644 \u0627\u0644\u0637\u0644\u0628\u0627\u062a \u0627\u0644\u0622\u0646.\u00bb \u0627\u062e\u062a\u0631 \u0645\u0646\u062a\u062c\u0627\u064b \u0641\u064a \u0627\u0644\u0643\u062a\u0644\u0629\u060c \u0623\u0648 \u0627\u0646\u0634\u0631 \u0639\u0644\u0649 \u0623\u064a \u062d\u0627\u0644." },
  // Card t_983425e5: the product picker's placeholder. Without a real empty
  // option the browser displays the FIRST product while the stored value is "".
  "bld.chooseProduct": { en: "Choose a product\u2026", ms: "Pilih produk\u2026", zh: "\u9009\u62e9\u5546\u54c1\u2026", ta: "\u0b92\u0bb0\u0bc1 \u0baa\u0bca\u0bb0\u0bc1\u0bb3\u0bc8\u0ba4\u0bcd \u0ba4\u0bc7\u0bb0\u0bcd\u0b9a\u0bcd\u0b9a\u0bc6\u0baf\u0bcd\u0b95\u2026", ar: "\u0627\u062e\u062a\u0631 \u0645\u0646\u062a\u062c\u0627\u064b\u2026" },
  // Card t_983425e5: the link inside that warning, pointing at the block.
  "bld.goToProductBlock": { en: "Pick a product in this block", ms: "Pilih produk dalam blok ini", zh: "\u5728\u6b64\u5757\u4e2d\u9009\u62e9\u5546\u54c1", ta: "\u0b87\u0ba8\u0bcd\u0ba4\u0ba4\u0bcd \u0ba4\u0bca\u0b95\u0bc1\u0ba4\u0bbf\u0baf\u0bbf\u0bb2\u0bcd \u0baa\u0bca\u0bb0\u0bc1\u0bb3\u0bc8\u0ba4\u0bcd \u0ba4\u0bc7\u0bb0\u0bcd\u0b9a\u0bcd\u0b9a\u0bc6\u0baf\u0bcd\u0b95", ar: "\u0627\u062e\u062a\u0631 \u0645\u0646\u062a\u062c\u0627\u064b \u0641\u064a \u0647\u0630\u0647 \u0627\u0644\u0643\u062a\u0644\u0629" },
  // Card t_d5112f02: the OTHER answer to the same picker question. The block is
  // bound to a product that was archived (or moved to the bin): its row still
  // exists, so the buyer page keeps selling it from the form's saved snapshot and
  // the form still takes orders. This must never quote the buyer's "not accepting
  // orders" sentence — that is the false alarm this card removes.
  "bld.productArchivedWarn": { en: "A product block is bound to a product that is no longer on sale (archived, or in the bin), so it is not in the list above. Buyers still get it from this form's saved version, so the form still takes orders. Pick a product in the block if you want it on sale again.", ms: "Satu blok produk terikat pada produk yang tidak lagi dijual (diarkibkan, atau dalam tong sampah), jadi ia tiada dalam senarai di atas. Pembeli masih menerimanya daripada versi borang yang tersimpan, jadi borang ini masih menerima pesanan — pilih produk dalam blok itu jika mahu ia dijual semula.", zh: "某个商品块绑定的商品已停售（已归档，或在回收站中），因此上方列表里没有它。买家仍可通过此表单已保存的版本购买，所以表单仍在接受订单——如果想重新上架，请在该块中选择商品。", ta: "ஒரு தயாரிப்புத் தொகுதி இனி விற்பனையில் இல்லாத (காப்பகப்படுத்தப்பட்ட அல்லது குப்பையில் உள்ள) தயாரிப்புடன் இணைக்கப்பட்டுள்ளது, எனவே மேலே உள்ள பட்டியலில் அது இல்லை. வாங்குபவர் இந்தப் படிவத்தின் சேமிக்கப்பட்ட பதிப்பிலிருந்து அதைப் பெறுவார்கள், எனவே படிவம் இன்னும் ஆர்டர்களை ஏற்கிறது — மீண்டும் விற்பனைக்கு வைக்க வேண்டுமெனில் அந்தத் தொகுதியில் ஒரு தயாரிப்பைத் தேர்ந்தெடுக்கவும்.", ar: "كتلة منتج مرتبطة بمنتج لم يعد معروضاً للبيع (مؤرشف أو في سلة المهملات)، لذا لا يظهر في القائمة أعلاه. لا يزال المشترون يحصلون عليه من النسخة المحفوظة من هذا النموذج، لذا ما زال النموذج يستقبل الطلبات — اختر منتجاً في الكتلة إن أردت إعادته للبيع." },
  "forms.bulkNoProductsConfirm": { en: "{n} selected form(s) are payment forms with no products, so they cannot take money yet. Publish them anyway?", ms: "{n} borang dipilih ialah borang bayaran tanpa produk, jadi ia belum boleh menerima bayaran. Teruskan publish?", zh: "所选表单中有 {n} 个是没有商品的付款表单，因此暂时无法收款。仍然发布吗？", ta: "தேர்ந்தெடுத்த {n} படிவம் தயாரிப்புகள் இல்லாத கட்டணப் படிவம், எனவே இன்னும் பணம் பெற முடியாது. இருந்தாலும் வெளியிடவா?", ar: "عدد {n} من النماذج المحددة نماذج دفع بلا منتجات، لذا لا يمكنها تحصيل المدفوعات بعد. هل تريد نشرها على أي حال؟" },
  "bld.dragToAdd": { en: "Drag to add", ms: "Tarik untuk tambah", zh: "拖动添加", ta: "இழுத்து சேர்க்க", ar: "اسحب للإضافة" },
  "bld.dragHere": { en: "Drag blocks here, or click one on the left.", ms: "Tarik blok ke sini — atau klik satu di kiri.", zh: "将模块拖到这里 — 或点击左侧的模块。", ta: "பிளாக்குகளை இங்கே இழுக்கவும் — அல்லது இடதுபுறம் ஒன்றைக் கிளிக் செய்யவும்.", ar: "اسحب العناصر إلى هنا — أو انقر على أحد العناصر على اليسار." },
  "bld.blockSettings": { en: "Block settings", ms: "Tetapan blok", zh: "模块设置", ta: "பிளாக் அமைப்புகள்", ar: "إعدادات العنصر" },
  "bld.withdrawnBlock": { en: "No longer offered in the builder. Buyers see this label and a note, not a field.", ms: "Tidak lagi ditawarkan dalam pembina. Pembeli melihat label ini dan satu nota — bukan medan.", zh: "构建器中已不再提供。买家只会看到这个标签和一条说明——不是输入字段。", ta: "பில்டரில் இனி வழங்கப்படுவதில்லை. வாங்குபவர்கள் இந்த லேபிளையும் ஒரு குறிப்பையும் மட்டுமே காண்பார்கள் — புலம் அல்ல.", ar: "لم يعد متاحاً في المنشئ. يرى المشترون هذا العنوان وملاحظة — وليس حقل إدخال." },
  "bld.selectBlock": { en: "Select a block to edit.", ms: "Pilih blok untuk edit.", zh: "选择要编辑的模块。", ta: "திருத்த ஒரு பிளாக்கைத் தேர்ந்தெடுக்கவும்.", ar: "اختر عنصراً للتحرير." },
  "bld.textLabel": { en: "Text / label", ms: "Teks / label", zh: "文本 / 标签", ta: "உரை / லேபிள்", ar: "النص / التسمية" },
  // Card t_4823943a: a `text` block is a short-answer INPUT, so its value is the
  // field's LABEL. Forms written under the old content model stored their intro
  // sentence there and the live page paints that sentence as the label of an
  // empty input. These four keys are the warning shown where the block is
  // edited, the explicit action that retypes the block to `section`, and its
  // exact inverse — never a silent rewrite.
  "bld.textProseWarn": { en: "This reads like an introduction, but a Text block is an answer field, so on the live form this sentence becomes the field's label. If it is an introduction, treat it as an intro line.", ms: "Ini kelihatan seperti pengenalan, tetapi blok Teks ialah medan jawapan — pada borang langsung ayat ini menjadi label medan. Jika ia pengenalan, jadikannya baris pengenalan.", zh: "这段文字看起来像引言，但文本区块是作答字段——在正式表单上这句话会成为字段的标签。如果它是引言，请将其设为引言行。", ta: "இது அறிமுக உரை போலத் தெரிகிறது, ஆனால் உரை தொகுதி ஒரு பதில் புலம் — நேரடி படிவத்தில் இந்த வாக்கியம் புலத்தின் லேபலாக மாறும். இது அறிமுகமெனில், அதை அறிமுக வரியாக மாற்றுங்கள்.", ar: "يبدو هذا كمقدمة، لكن كتلة النص حقل إجابة — في النموذج المنشور تصبح هذه الجملة تسمية الحقل. إن كانت مقدمة، فحوّلها إلى سطر مقدمة." },
  "bld.textProseTreat": { en: "Treat as intro line", ms: "Jadikan baris pengenalan", zh: "设为引言行", ta: "அறிமுக வரியாக மாற்று", ar: "تحويل إلى سطر مقدمة" },
  "bld.textProseConverted": { en: "Converted: this block is now an intro line, so the field is gone from the live form. The text you wrote is unchanged.", ms: "Ditukar: blok ini kini baris pengenalan, jadi medan itu tiada lagi pada borang langsung. Teks anda tidak diubah.", zh: "已转换：该区块现在是引言行，字段已从正式表单中移除。你写的文字未作改动。", ta: "மாற்றப்பட்டது: இந்தத் தொகுதி இப்போது அறிமுக வரி, எனவே புலம் நேரடி படிவத்தில் இல்லை. நீங்கள் எழுதிய உரை மாறவில்லை.", ar: "تم التحويل: أصبحت هذه الكتلة سطر مقدمة، لذا اختفى الحقل من النموذج المنشور. النص الذي كتبته لم يتغير." },
  "bld.textProseUndo": { en: "Undo. Make it a field again", ms: "Buat asal — jadikan medan semula", zh: "撤销——恢复为字段", ta: "செயல்நீக்கம் — மீண்டும் புலமாக்கு", ar: "تراجع — أعده حقلاً" },
  "bld.imageUrl": { en: "Image URL", ms: "URL imej", zh: "图片链接", ta: "படம் URL", ar: "رابط الصورة" },
  "bld.product": { en: "Product", ms: "Produk", zh: "商品", ta: "பொருள்", ar: "المنتج" },
  "bld.required": { en: "Required", ms: "Wajib", zh: "必填", ta: "கட்டாயம்", ar: "مطلوب" },
  "bld.condLogic": { en: "Conditional logic (optional)", ms: "Logik bersyarat (pilihan)", zh: "条件逻辑（可选）", ta: "நிபந்தனை தர்க்கம் (விருப்பம்)", ar: "المنطق الشرطي (اختياري)" },
  "bld.alwaysShow": { en: "Always show", ms: "— Sentiasa tunjuk —", zh: "— 始终显示 —", ta: "— எப்போதும் காட்டு —", ar: "— اعرض دائماً —" },
  // Card t_a81a9165: a stored conditional rule whose `field_id` is not an option
  // the picker can offer. The id must reach the merchant (so they can see which
  // reference broke), and the notice has to state the LIVE consequence, because
  // the old behaviour was the opposite claim: the block read as "always show"
  // while the server kept it hidden and discarded its answers.
  "bld.condOptionUnavailable": { en: "⚠ Not available: {id}", ms: "⚠ Tidak tersedia: {id}", zh: "⚠ 不可用：{id}", ta: "⚠ கிடைக்கவில்லை: {id}", ar: "⚠ غير متاح: {id}" },
  "bld.condWarnUnavailable": { en: "This block's rule points at a field that is not available on this form ({id}). On the live form the rule never matches, so this block stays hidden and any answer to it is discarded. Pick a field above, or choose “Always show” to remove the rule.", ms: "Peraturan blok ini menunjuk kepada medan yang tidak tersedia pada borang ini ({id}). Pada borang langsung peraturan ini tidak pernah padan, jadi blok ini kekal tersembunyi dan sebarang jawapan kepadanya dibuang. Pilih medan di atas, atau pilih “Sentiasa tunjuk” untuk membuang peraturan ini.", zh: "此区块的规则指向本表单中不可用的字段（{id}）。在正式表单上该规则永不匹配，因此该区块会一直隐藏，提交给它的答案会被丢弃。请在上方选择一个字段，或选择“始终显示”以删除该规则。", ta: "இந்தத் தொகுதியின் விதி இந்தப் படிவத்தில் கிடைக்காத புலத்தைச் சுட்டுகிறது ({id}). நேரடி படிவத்தில் இந்த விதி ஒருபோதும் பொருந்தாது, எனவே இந்தத் தொகுதி மறைந்திருக்கும், அதற்கான பதில்கள் நிராகரிக்கப்படும். மேலே ஒரு புலத்தைத் தேர்ந்தெடுக்கவும், அல்லது விதியை நீக்க “எப்போதும் காட்டு” தேர்ந்தெடுக்கவும்.", ar: "تشير قاعدة هذه الكتلة إلى حقل غير متاح في هذا النموذج ({id}). في النموذج المنشور لا تتطابق القاعدة أبداً، لذا تبقى هذه الكتلة مخفية وتُهمل أي إجابة عليها. اختر حقلاً من الأعلى، أو اختر «اعرض دائماً» لإزالة القاعدة." },
  "bld.condBrokenBadge": { en: "rule broken", ms: "peraturan rosak", zh: "规则失效", ta: "விதி பழுது", ar: "القاعدة معطلة" },
  "bld.displaySettings": { en: "Display settings", ms: "Tetapan paparan", zh: "显示设置", ta: "காட்சி அமைப்புகள்", ar: "إعدادات العرض" },
  "bld.themeColor": { en: "Theme colour (primary)", ms: "Warna tema (primer)", zh: "主题色（主色）", ta: "தீம் நிறம் (முதன்மை)", ar: "لون السمة (أساسي)" },
  "bld.logoUrl": { en: "Logo URL (https)", ms: "URL logo (https)", zh: "Logo 链接（https）", ta: "லோகோ URL (https)", ar: "رابط الشعار (https)" },
  // issue #391: form logo media picker (WordPress-style)
  "mediaPicker.title": { en: "Select logo", ms: "Pilih logo", zh: "选择 Logo", ta: "லோகோ தேர்ந்தெடுக்கவும்", ar: "اختر الشعار" },
  "mediaPicker.uploadNew": { en: "Upload new", ms: "Muat naik baharu", zh: "上传新图片", ta: "புதியதாக பதிவேற்று", ar: "رفع جديد" },
  "mediaPicker.pasteUrl": { en: "Paste URL", ms: "Tampal URL", zh: "粘贴链接", ta: "URL ஒட்டு", ar: "لصق الرابط" },
  "mediaPicker.useUrl": { en: "Use this URL", ms: "Guna URL ini", zh: "使用此链接", ta: "இந்த URL பயன்படுத்து", ar: "استخدم هذا الرابط" },
  "mediaPicker.choose": { en: "Choose from media library", ms: "Pilih dari perpustakaan media", zh: "从媒体库选择", ta: "ஊடக நூலகத்திலிருந்து தேர்ந்தெடுக்கவும்", ar: "اختر من مكتبة الوسائط" },
  "mediaPicker.remove": { en: "Remove logo", ms: "Buang logo", zh: "移除 Logo", ta: "லோகோ நீக்கு", ar: "إزالة الشعار" },
  "mediaPicker.noLogo": { en: "No logo selected", ms: "Tiada logo dipilih", zh: "未选择 Logo", ta: "லோகோ தேர்ந்தெடுக்கப்படவில்லை", ar: "لم يتم اختيار شعار" },
  "mediaPicker.close": { en: "Close", ms: "Tutup", zh: "关闭", ta: "மூடு", ar: "إغلاق" },
  "bld.redirSuccess": { en: "Custom success redirect (optional)", ms: "Redirect selepas berjaya (pilihan)", zh: "自定义成功跳转（可选）", ta: "வெற்றி திருப்புதல் (விருப்பம்)", ar: "توجيه النجاح المخصص (اختياري)" },
  "bld.redirFailure": { en: "Custom failure redirect (optional)", ms: "Redirect selepas gagal (pilihan)", zh: "自定义失败跳转（可选）", ta: "தோல்வி திருப்புதல் (விருப்பம்)", ar: "توجيه الفشل المخصص (اختياري)" },
  "bld.allowEmbed": { en: "Allow embedding (iframe)", ms: "Benarkan embed (iframe)", zh: "允许嵌入（iframe）", ta: "எம்பெட் அனுமதி (iframe)", ar: "السماح بالتضمين (iframe)" },
  "bld.embedDocs": { en: "Embed guide (auto-height)", ms: "Panduan embed (tinggi auto)", zh: "嵌入指南（自动高度）", ta: "எம்பெட் வழிகாட்டி (தானியங்கி உயரம்)", ar: "دليل التضمين (ارتفاع تلقائي)" },
  "bld.saveDisplay": { en: "Save display settings", ms: "Simpan tetapan paparan", zh: "保存显示设置", ta: "காட்சி அமைப்புகளைச் சேமி", ar: "حفظ إعدادات العرض" },
  "bld.displaySaved": { en: "Display settings saved ✓", ms: "Tetapan paparan disimpan ✓", zh: "显示设置已保存 ✓", ta: "காட்சி அமைப்புகள் சேமிக்கப்பட்டன ✓", ar: "تم حفظ إعدادات العرض ✓" },
  "bld.restrictionsTitle": { en: "Restrictions", ms: "Sekatan", zh: "限制", ta: "கட்டுப்பாடுகள்", ar: "القيود" },
  "bld.scheduleTitle": { en: "Schedule", ms: "Jadual", zh: "时间安排", ta: "அட்டவணை", ar: "الجدول الزمني" },
  "bld.scheduleHint": { en: "Limit when this form is open. Leave blank for no schedule.", ms: "Hadkan bila borang ini dibuka. Kosongkan jika tiada jadual.", zh: "限制此表单的开放时间。留空表示无时间安排。", ta: "இந்த படிவம் எப்போது திறக்கப்படும் என்பதை வரம்பிடவும். அட்டவணை இல்லை எனில் காலியாக விடவும்.", ar: "حدد متى يكون هذا النموذج مفتوحاً. اتركه فارغاً إذا لم يكن هناك جدول." },
  "bld.startAt": { en: "Start date & time", ms: "Tarikh & masa mula", zh: "开始日期和时间", ta: "தொடக்க தேதி & நேரம்", ar: "تاريخ ووقت البدء" },
  "bld.endAt": { en: "End date & time", ms: "Tarikh & masa tamat", zh: "结束日期和时间", ta: "முடிவு தேதி & நேரம்", ar: "تاريخ ووقت الانتهاء" },
  "bld.pendingMessage": { en: "Pending message (before start)", ms: "Mesej menunggu (sebelum mula)", zh: "待开放消息（开始前）", ta: "காத்திருப்பு செய்தி (தொடக்கத்திற்கு முன்)", ar: "رسالة الانتظار (قبل البدء)" },
  "bld.expiredMessage": { en: "Expired message (after end)", ms: "Mesej tamat (selepas tamat)", zh: "已过期消息（结束后）", ta: "காலாவதி செய்தி (முடிவுக்குப் பிறகு)", ar: "رسالة انتهاء الصلاحية (بعد الانتهاء)" },
  "bld.entryLimitTitle": { en: "Entry limit", ms: "Had penyertaan", zh: "提交上限", ta: "பதிவு வரம்பு", ar: "حد الردود" },
  "bld.entryLimitHint": { en: "Maximum number of entries. Leave blank for no limit.", ms: "Maksimum bilangan penyertaan. Kosongkan jika tiada had.", zh: "最大提交数。留空表示无限制。", ta: "அதிகபட்ச பதிவு எண்ணிக்கை. வரம்பு இல்லை எனில் காலியாக விடவும்.", ar: "الحد الأقصى لعدد الردود. اتركه فارغاً إذا لم يكن هناك حد." },
  "bld.entryLimit": { en: "Max entries", ms: "Maksimum penyertaan", zh: "最大提交数", ta: "அதிகபட்ச பதிவுகள்", ar: "الحد الأقصى للردود" },
  "bld.limitMessage": { en: "Limit-reached message", ms: "Mesej had tercapai", zh: "已达上限消息", ta: "வரம்பு அடைந்த செய்தி", ar: "رسالة بلوغ الحد" },
  "bld.saveRestrictions": { en: "Save restrictions", ms: "Simpan sekatan", zh: "保存限制", ta: "கட்டுப்பாடுகளைச் சேமி", ar: "حفظ القيود" },
  "bld.restrictionsSaved": { en: "Restrictions saved ✓", ms: "Sekatan disimpan ✓", zh: "限制已保存 ✓", ta: "கட்டுப்பாடுகள் சேமிக்கப்பட்டன ✓", ar: "تم حفظ القيود ✓" },
  "bld.embedOnSite": { en: "Embed on your site", ms: "Embed di laman anda", zh: "嵌入到您的网站", ta: "உங்கள் தளத்தில் எம்பெட்", ar: "التضمين في موقعك" },
  "bld.copyEmbed": { en: "Copy embed code", ms: "Salin kod embed", zh: "复制嵌入代码", ta: "எம்பெட் கோடை நகலெடு", ar: "نسخ كود التضمين" },
  "bld.embedCopied": { en: "Embed code copied ✓", ms: "Kod embed disalin ✓", zh: "嵌入代码已复制 ✓", ta: "எம்பெட் கோட் நகலெடுக்கப்பட்டது ✓", ar: "تم نسخ كود التضمين ✓" },
  "bld.trackingTitle": { en: "Tracking (this form)", ms: "Penjejakan (borang ini)", zh: "跟踪（此表单）", ta: "கண்காணிப்பு (இந்த படிவம்)", ar: "التتبع (هذا النموذج)" },
  "bld.saveFormTracking": { en: "Save form tracking", ms: "Simpan penjejakan borang", zh: "保存表单跟踪", ta: "படிவ கண்காணிப்பைச் சேமி", ar: "حفظ تتبع النموذج" },
  "bld.formTrackingSaved": { en: "Form tracking saved ✓", ms: "Penjejakan borang disimpan ✓", zh: "表单跟踪已保存 ✓", ta: "படிவ கண்காணிப்பு சேமிக்கப்பட்டது ✓", ar: "تم حفظ تتبع النموذج ✓" },
  "bld.thisFormSoFar": { en: "This form so far", ms: "Borang ini setakat ini", zh: "此表单数据", ta: "இந்த படிவம் இதுவரை", ar: "هذا النموذج حتى الآن" },
  "bld.confirmedRevenue": { en: "confirmed revenue", ms: "hasil disahkan", zh: "已确认收入", ta: "உறுதிப்படுத்தப்பட்ட வருவாய்", ar: "الإيرادات المؤكدة" },
  "bld.salesThisForm": { en: "Sales (this form)", ms: "Jualan (borang ini)", zh: "销售（此表单）", ta: "விற்பனை (இந்த படிவம்)", ar: "المبيعات (هذا النموذج)" },
  "bld.actions": { en: "Actions", ms: "Tindakan", zh: "操作", ta: "செயல்கள்", ar: "إجراءات" },
  "bld.captureAll": { en: "Capture all pending", ms: "Capture semua menunggu", zh: "捕获全部待处理", ta: "காத்திருப்பை அனைத்தையும் கேப்சர்", ar: "تحصيل الكل المعلق" },
  "bld.capture": { en: "Capture", ms: "Capture", zh: "捕获", ta: "கேப்சர்", ar: "تحصيل" },
  "bld.release": { en: "Release", ms: "Release", zh: "释放", ta: "விடுவி", ar: "إفراج" },
  "bld.noSalesForm": { en: "No sales yet for this form.", ms: "Belum ada jualan untuk borang ini.", zh: "此表单暂无销售。", ta: "இந்த படிவத்திற்கு இன்னும் விற்பனை இல்லை.", ar: "لا مبيعات لهذا النموذج بعد." },
  "bld.existingForms": { en: "Existing forms", ms: "Borang sedia ada", zh: "现有表单", ta: "இருக்கும் படிவங்கள்", ar: "النماذج الحالية" },
  // issue #171: response-only form mode + responses view
  "bld.mode": { en: "Form mode", ms: "Mod borang", zh: "表单模式", ta: "படிவ முறை", ar: "وضع النموذج" },
  "bld.modePayment": { en: "Payment form", ms: "Borang bayaran", zh: "付款表单", ta: "கட்டண படிவம்", ar: "نموذج الدفع" },
  "bld.modeResponse": { en: "Response form", ms: "Borang respons", zh: "响应表单", ta: "பதில் படிவம்", ar: "نموذج الرد" },
  "bld.modePaymentHint": { en: "Collect payments with products (current behaviour).", ms: "Kutip bayaran dengan produk (tingkah laku semasa).", zh: "通过商品收款（当前行为）。", ta: "தயாரிப்புகளுடன் கட்டணங்களை வசூலிக்கவும் (தற்போதைய நடத்தை).", ar: "تحصيل المدفوعات مع المنتجات (السلوك الحالي)." },
  "bld.modeResponseHint": { en: "Collect responses only. No products, no payment.", ms: "Kutip respons sahaja — tiada produk, tiada bayaran.", zh: "仅收集响应 — 无商品、无付款。", ta: "பதில்களை மட்டும் சேகரிக்கவும் — தயாரிப்புகள் இல்லை, கட்டணம் இல்லை.", ar: "جمع الردود فقط — بدون منتجات وبدون دفع." },
  "bld.modeDocs": { en: "How the form mode changes the default notification", ms: "Bagaimana mod borang mengubah notifikasi lalai", zh: "表单模式如何更改默认通知", ta: "படிவ பயன்முறை இயல்புநிலை அறிவிப்பை எவ்வாறு மாற்றுகிறது", ar: "كيف يغيّر وضع النموذج الإشعار الافتراضي" },
  // Card t_6dbc1e16: the mode now switches from a header dialog, and that
  // dialog states the two consequences a merchant cannot see from two buttons.
  // {n} = how many payment blocks are already placed; {event} = the event the
  // new mode's default notification fires.
  "bld.modePricingNote": { en: "{n} payment block(s) on this form carry a price, a quantity, an add-on, shipping or an order total. A response form does not draw them, so the buyer never sees them and no money is collected for them.", ms: "{n} blok bayaran pada borang ini membawa harga, kuantiti, tambahan, penghantaran atau jumlah pesanan. Borang respons tidak memaparkannya, jadi pembeli tidak nampak dan tiada wang dikutip untuknya.", zh: "此表单有 {n} 个付款模块带有价格、数量、附加项、运费或订单总额。响应表单不会绘制它们，因此买家看不到，也不会为其付款。", ta: "இந்தப் படிவத்தில் விலை, அளவு, கூடுதல், டெலிவரி கட்டணம் அல்லது மொத்தத் தொகையைக் கொண்ட {n} கட்டண தொகுதிகள் உள்ளன. பதில் படிவம் அவற்றை வரையாது, எனவே வாங்குபவர் அவற்றைப் பார்க்க மாட்டார், அவற்றுக்கு பணமும் வசூலாகாது.", ar: "يحتوي هذا النموذج على {n} كتلة دفع تحمل سعراً أو كمية أو إضافة أو شحناً أو إجمالي طلب. نموذج الرد لا يرسمها، فلا يراها المشتري ولا يُحصَّل مال مقابلها." },
  "bld.modeNotifyNote": { en: "Saving re-points this form's untouched default notification to the event the new mode fires ({event}). A rule you have edited is left alone.", ms: "Menyimpan akan mengalihkan notifikasi lalai borang ini yang belum diubah ke peristiwa mod baharu ({event}). Peraturan yang anda telah ubah tidak disentuh.", zh: "保存后，此表单未修改的默认通知将改为新模式的触发事件（{event}）。您已编辑过的规则不会被改动。", ta: "சேமிக்கும்போது இந்தப் படிவத்தின் மாற்றப்படாத இயல்புநிலை அறிவிப்பு புதிய பயன்முறையின் நிகழ்வுக்கு ({event}) மாற்றப்படும். நீங்கள் திருத்திய விதி தொடப்படாது.", ar: "سيؤدي الحفظ إلى إعادة توجيه الإشعار الافتراضي غير المعدَّل لهذا النموذج إلى الحدث الذي يطلقه الوضع الجديد ({event}). القاعدة التي عدّلتها لا تُمَس." },
  "bld.responsesThisForm": { en: "Responses (this form)", ms: "Respons (borang ini)", zh: "响应（此表单）", ta: "பதில்கள் (இந்த படிவம்)", ar: "الردود (هذا النموذج)" },
  "bld.noResponsesForm": { en: "No responses yet for this form.", ms: "Belum ada respons untuk borang ini.", zh: "此表单暂无响应。", ta: "இந்த படிவத்திற்கு இன்னும் பதில்கள் இல்லை.", ar: "لا توجد ردود لهذا النموذج بعد." },
  "bld.loadResponsesErr": { en: "Failed to load responses.", ms: "Gagal memuatkan respons.", zh: "加载响应失败。", ta: "பதில்களை ஏற்ற முடியவில்லை.", ar: "فشل تحميل الردود." },
  "bld.responseCount": { en: "{n} responses", ms: "{n} respons", zh: "{n} 条响应", ta: "{n} பதில்கள்", ar: "{n} رد" },
  "bld.responses": { en: "Responses", ms: "Respons", zh: "响应", ta: "பதில்கள்", ar: "الردود" },
  "bld.responseData": { en: "Data", ms: "Data", zh: "数据", ta: "தரவு", ar: "البيانات" },
  // Issue #604: the Sales page renders a response-mode form's submissions in the
  // same table shell, so it needs its own header/columns/empty state.
  "bld.responsesSubtitle": { en: "Every response recorded for this form.", ms: "Semua respons yang direkodkan untuk borang ini.", zh: "此表单记录的每一条响应。", ta: "இந்த படிவத்திற்குப் பதிவான ஒவ்வொரு பதிலும்.", ar: "كل رد مسجَّل لهذا النموذج." },
  "bld.responsesEmpty": { en: "No responses yet for this form.", ms: "Belum ada respons untuk borang ini.", zh: "此表单暂无响应。", ta: "இந்த படிவத்திற்கு இன்னும் பதில்கள் இல்லை.", ar: "لا توجد ردود لهذا النموذج بعد." },
  "bld.responseAnswers": { en: "Answers", ms: "Jawapan", zh: "回答", ta: "பதில்கள்", ar: "الإجابات" },
  "bld.responseReceived": { en: "Received", ms: "Diterima", zh: "收到时间", ta: "பெறப்பட்டது", ar: "الاستلام" },
  "bld.responseSubmittedAt": { en: "Submitted", ms: "Dihantar", zh: "提交于", ta: "சமர்ப்பிக்கப்பட்டது", ar: "تم الإرسال" },
  "bld.chipOverride": { en: "CHIP override (this form)", ms: "CHIP override (borang ini)", zh: "CHIP 覆盖（此表单）", ta: "CHIP மேலெழுதல் (இந்த படிவம்)", ar: "تجاوز CHIP (هذا النموذج)" },
  "bld.emptyDefaults": { en: "Empty = use account defaults.", ms: "Kosong = guna tetapan lalai akaun.", zh: "留空 = 使用账户默认设置。", ta: "காலியாக = கணக்கின் இயல்புநிலையைப் பயன்படுத்தவும்.", ar: "فارغ = استخدم الإعدادات الافتراضية للحساب." },
  "bld.saveOverride": { en: "Save override", ms: "Simpan override", zh: "保存覆盖", ta: "மேலெழுதலைச் சேமி", ar: "حفظ التجاوز" },
  "bld.overrideSaved": { en: "Override saved ✓", ms: "Override disimpan ✓", zh: "覆盖已保存 ✓", ta: "மேலெழுதல் சேமிக்கப்பட்டது ✓", ar: "تم حفظ التجاوز ✓" },
  "bld.overrideCleared": { en: "Override cleared. Account defaults apply", ms: "Override dikosongkan — tetapan lalai akaun terpakai", zh: "覆盖已清除 — 应用账户默认设置", ta: "மேலெழுதல் அழிக்கப்பட்டது — கணக்கின் இயல்புநிலை பயன்படும்", ar: "تم مسح التجاوز — تنطبق الإعدادات الافتراضية للحساب" },
  "bld.failedOverride": { en: "Failed to save override", ms: "Gagal simpan override", zh: "保存覆盖失败", ta: "மேலெழுதலைச் சேமிக்க தோல்வி", ar: "فشل حفظ التجاوز" },
  "bld.chipMapping": { en: "CHIP client field mapping", ms: "Peta medan pelanggan CHIP", zh: "CHIP 客户字段映射", ta: "CHIP வாடிக்கையாளர் புல வரைபடம்", ar: "تعيين حقول عميل CHIP" },
  "bld.chipMappingHint": { en: "Pick which form block supplies each CHIP client field. Empty = default (name→full_name, email→email, phone→phone).", ms: "Pilih blok borang yang membekalkan setiap medan pelanggan CHIP. Kosong = lalai (name→full_name, email→email, phone→phone).", zh: "选择哪个表单块为每个 CHIP 客户字段提供值。留空 = 默认（name→full_name，email→email，phone→phone）。", ta: "ஒவ்வொரு CHIP வாடிக்கையாளர் புலத்திற்கும் எந்த படிவத் தொகுதி வழங்குகிறது என்பதைத் தேர்ந்தெடுக்கவும். காலியாக = இயல்புநிலை (name→full_name, email→email, phone→phone).", ar: "اختر كتلة النموذج التي تزود كل حقل عميل CHIP. فارغ = الافتراضي (name→full_name، email→email، phone→phone)." },
  "bld.chipMappingDefault": { en: "Default", ms: "Lalai", zh: "默认", ta: "இயல்புநிலை", ar: "الافتراضي" },
  "bld.chipMappingSavedOnSave": { en: "Saved when you save the form.", ms: "Disimpan apabila anda simpan borang.", zh: "保存表单时保存。", ta: "படிவத்தைச் சேமிக்கும்போது சேமிக்கப்படும்.", ar: "يُحفظ عند حفظ النموذج." },
  // issue #114: builder error/success fallbacks that were hardcoded BM
  "bld.loadSalesErr": { en: "Failed to load sales.", ms: "Gagal memuatkan jualan.", zh: "加载销售失败。", ta: "விற்பனையை ஏற்ற முடியவில்லை.", ar: "فشل تحميل المبيعات." },
  "bld.bulkCaptureFail": { en: "Bulk capture failed.", ms: "Bulk capture gagal.", zh: "批量捕获失败。", ta: "மொத்த கேப்சர் தோல்வி.", ar: "فشل التحصيل الجماعي." },
  "bld.bulkCaptureDone": { en: "Captured {captured}/{requested}. {failed} failed.", ms: "Captured {captured}/{requested} — {failed} gagal.", zh: "已捕获 {captured}/{requested} — {failed} 失败。", ta: "கேப்சர் {captured}/{requested} — {failed} தோல்வி.", ar: "تم التحصيل {captured}/{requested} — فشل {failed}." },
  "bld.bulkCaptureCheck": { en: " Check each status.", ms: " Semak status setiap satu.", zh: " 请检查每个状态。", ta: " ஒவ்வொரு நிலையையும் சரிபார்க்கவும்.", ar: " تحقق من كل حالة." },
  "bld.uploadImageFail": { en: "Image upload failed.", ms: "Muat naik imej gagal.", zh: "图片上传失败。", ta: "பட பதிவேற்றம் தோல்வி.", ar: "فشل رفع الصورة." },
  "bld.moveUp": { en: "Move up", ms: "Naik", zh: "上移", ta: "மேலே நகர்த்து", ar: "نقل لأعلى" },
  "bld.moveDown": { en: "Move down", ms: "Turun", zh: "下移", ta: "கீழே நகர்த்து", ar: "نقل لأسفل" },
  "bld.removeBlock": { en: "Remove", ms: "Buang", zh: "移除", ta: "நீக்கு", ar: "إزالة" },
  "bld.phText": { en: "Text…", ms: "Teks…", zh: "文本…", ta: "உரை…", ar: "نص…" },
  "bld.phImage": { en: "Image URL →", ms: "URL imej →", zh: "图片链接 →", ta: "படம் URL →", ar: "رابط الصورة →" },
  "bld.phField": { en: "Field", ms: "Medan", zh: "字段", ta: "புலம்", ar: "حقل" },
  // issue #59: mobile section switcher labels
  "bld.addBlock": { en: "Add", ms: "Tambah", zh: "添加", ta: "சேர்", ar: "إضافة" },
  "bld.canvas": { en: "Canvas", ms: "Canvas", zh: "画布", ta: "கேன்வாஸ்", ar: "اللوحة" },
  "bld.settings": { en: "Settings", ms: "Tetapan", zh: "设置", ta: "அமைப்புகள்", ar: "الإعدادات" },
  // issue #61: image upload labels
  "bld.uploadImage": { en: "Upload image", ms: "Muat naik imej", zh: "上传图片", ta: "படத்தைப் பதிவேற்று", ar: "رفع صورة" },
  "bld.uploading": { en: "Uploading…", ms: "Memuat naik…", zh: "上传中…", ta: "பதிவேற்றுகிறது…", ar: "جارٍ الرفع…" },
  // The image block's client-side pre-flight refusal. It was a hardcoded Malay
  // sentence, so a merchant on any other dashboard language read Malay for a
  // file the browser could have told them about before the upload. The list
  // mirrors the file input's `accept` and objstore.SniffImageType — the server
  // sniffs the real bytes and is still the authority.
  "bld.imageTypeUnsupported": { en: "That image type is not supported (PNG/JPEG/WebP/GIF only).", ms: "Jenis imej itu tidak disokong (PNG/JPEG/WebP/GIF sahaja).", zh: "不支持该图片类型（仅限 PNG/JPEG/WebP/GIF）。", ta: "அந்த பட வகை ஆதரிக்கப்படவில்லை (PNG/JPEG/WebP/GIF மட்டும்).", ar: "نوع الصورة هذا غير مدعوم (PNG/JPEG/WebP/GIF فقط)." },
  // palette block labels
  "blk.text": { en: "Text", ms: "Teks", zh: "文本", ta: "உரை", ar: "نص" },
  "blk.textHint": { en: "Short answer / one line", ms: "Jawapan pendek / satu baris", zh: "简短回答 / 单行", ta: "குறுகிய பதில் / ஒரு வரி", ar: "إجابة قصيرة / سطر واحد" },
  "blk.image": { en: "Image", ms: "Imej", zh: "图片", ta: "படம்", ar: "صورة" },
  "blk.imageHint": { en: "Product photo / banner", ms: "Foto produk / banner", zh: "商品图片 / 横幅", ta: "பொருள் படம் / பேனர்", ar: "صورة منتج / لافتة" },
  "blk.product": { en: "Product", ms: "Produk", zh: "商品", ta: "பொருள்", ar: "منتج" },
  "blk.productHint": { en: "Buyable item with price", ms: "Item berharga boleh dibeli", zh: "带价格的可购商品", ta: "விலையுள்ள வாங்கக்கூடிய பொருள்", ar: "عنصر قابل للشراء بسعر" },
  "blk.name": { en: "Name", ms: "Nama", zh: "姓名", ta: "பெயர்", ar: "الاسم" },
  "blk.nameHint": { en: "Buyer name field", ms: "Medan nama pembeli", zh: "买家姓名字段", ta: "வாங்குபவர் பெயர் புலம்", ar: "حقل اسم المشتري" },
  "blk.email": { en: "Email", ms: "Emel", zh: "邮箱", ta: "மின்னஞ்சல்", ar: "البريد الإلكتروني" },
  "blk.emailHint": { en: "Buyer email field", ms: "Medan emel pembeli", zh: "买家邮箱字段", ta: "வாங்குபவர் மின்னஞ்சல் புலம்", ar: "حقل بريد المشتري" },
  "blk.phone": { en: "Phone", ms: "Telefon", zh: "电话", ta: "தொலைபேசி", ar: "الهاتف" },
  "blk.phoneHint": { en: "Phone (WhatsApp) field", ms: "Medan telefon (WhatsApp)", zh: "电话（WhatsApp）字段", ta: "தொலைபேசி (WhatsApp) புலம்", ar: "حقل الهاتف (واتساب)" },
  "blk.note": { en: "Note", ms: "Nota", zh: "备注", ta: "குறிப்பு", ar: "ملاحظة" },
  "blk.noteHint": { en: "Free-form message", ms: "Mesej bebas", zh: "自由填写的信息", ta: "சுதந்திரமான செய்தி", ar: "رسالة حرة" },
  "blk.checkbox": { en: "Checkbox", ms: "Kotak semak", zh: "复选框", ta: "தேர்வுப்பெட்டி", ar: "مربع اختيار" },
  "blk.checkboxHint": { en: "Extra option", ms: "Pilihan tambahan", zh: "附加选项", ta: "கூடுதல் விருப்பம்", ar: "خيار إضافي" },
  "blk.divider": { en: "Divider", ms: "Pembahagi", zh: "分隔线", ta: "பிரிப்பான்", ar: "فاصل" },
  "blk.dividerHint": { en: "Separator line", ms: "Garis pemisah", zh: "分隔线", ta: "பிரிப்பு கோடு", ar: "خط فاصل" },
  // issue #173: Gravity-Forms-style field palette + per-field settings
  "blk.textarea": { en: "Textarea", ms: "Teks", zh: "多行文本", ta: "உரைப்பகுதி", ar: "منطقة نص" },
  "blk.textareaHint": { en: "Multi-line text", ms: "Teks berbilang baris", zh: "多行文本", ta: "பல வரி உரை", ar: "نص متعدد الأسطر" },
  "blk.number": { en: "Number", ms: "Nombor", zh: "数字", ta: "எண்", ar: "رقم" },
  "blk.numberHint": { en: "Numeric input with min/max", ms: "Input nombor dengan min/maks", zh: "带最小/最大值的数字输入", ta: "குறைந்தபட்சம்/அதிகபட்சம் கொண்ட எண் உள்ளீடு", ar: "إدخال رقمي مع حد أدنى/أقصى" },
  "blk.select": { en: "Dropdown", ms: "Jatuh turun", zh: "下拉选择", ta: "கீழ்தோன்றல்", ar: "قائمة منسدلة" },
  "blk.selectHint": { en: "Choose one from a list", ms: "Pilih satu dari senarai", zh: "从列表中选择一项", ta: "பட்டியலில் இருந்து ஒன்றைத் தேர்ந்தெடுக்கவும்", ar: "اختر واحداً من قائمة" },
  "blk.radio": { en: "Radio buttons", ms: "Butang radio", zh: "单选按钮", ta: "ரேடியோ பொத்தான்கள்", ar: "أزرار اختيار" },
  "blk.radioHint": { en: "Choose one option", ms: "Pilih satu pilihan", zh: "选择一个选项", ta: "ஒரு விருப்பத்தைத் தேர்ந்தெடுக்கவும்", ar: "اختر خياراً واحداً" },
  "blk.date": { en: "Date", ms: "Tarikh", zh: "日期", ta: "தேதி", ar: "تاريخ" },
  "blk.dateHint": { en: "Date picker", ms: "Pemilih tarikh", zh: "日期选择器", ta: "தேதி தேர்வி", ar: "منتقي التاريخ" },
  "blk.time": { en: "Time", ms: "Masa", zh: "时间", ta: "நேரம்", ar: "الوقت" },
  "blk.timeHint": { en: "Time picker", ms: "Pemilih masa", zh: "时间选择器", ta: "நேர தேர்வி", ar: "منتقي الوقت" },
  "blk.website": { en: "Website", ms: "Laman web", zh: "网址", ta: "இணையதளம்", ar: "موقع ويب" },
  "blk.websiteHint": { en: "URL input", ms: "Input URL", zh: "网址输入", ta: "URL உள்ளீடு", ar: "إدخال رابط" },
  "blk.section": { en: "Section", ms: "Seksyen", zh: "分区", ta: "பிரிவு", ar: "قسم" },
  "blk.sectionHint": { en: "Section heading", ms: "Tajuk seksyen", zh: "分区标题", ta: "பிரிவு தலைப்பு", ar: "عنوان قسم" },
  "blk.html": { en: "HTML", ms: "HTML", zh: "HTML", ta: "HTML", ar: "HTML" },
  "blk.htmlHint": { en: "Custom HTML block", ms: "Blok HTML tersuai", zh: "自定义 HTML 模块", ta: "தனிப்பயன் HTML தொகுதி", ar: "كتلة HTML مخصصة" },
  "blk.address": { en: "Address", ms: "Alamat", zh: "地址", ta: "முகவரி", ar: "عنوان" },
  "blk.addressHint": { en: "Full address", ms: "Alamat penuh", zh: "完整地址", ta: "முழு முகவரி", ar: "عنوان كامل" },
  "blk.file": { en: "File upload", ms: "Muat naik fail", zh: "文件上传", ta: "கோப்பு பதிவேற்றம்", ar: "رفع ملف" },
  "blk.fileHint": { en: "Attach a file", ms: "Lampirkan fail", zh: "附加文件", ta: "கோப்பை இணை", ar: "إرفاق ملف" },
  "blk.consent": { en: "Consent", ms: "Persetujuan", zh: "同意", ta: "ஒப்புதல்", ar: "موافقة" },
  "blk.consentHint": { en: "Checkbox consent", ms: "Kotak semak persetujuan", zh: "同意复选框", ta: "ஒப்புதல் தேர்வுப்பெட்டி", ar: "مربع موافقة" },
  "blk.hidden": { en: "Hidden", ms: "Tersembunyi", zh: "隐藏字段", ta: "மறைக்கப்பட்டது", ar: "مخفي" },
  "blk.hiddenHint": { en: "Hidden value", ms: "Nilai tersembunyi", zh: "隐藏值", ta: "மறைக்கப்பட்ட மதிப்பு", ar: "قيمة مخفية" },
  "blk.list": { en: "List", ms: "Senarai", zh: "列表", ta: "பட்டியல்", ar: "قائمة" },
  "blk.listHint": { en: "List of items", ms: "Senarai item", zh: "项目列表", ta: "பொருட்களின் பட்டியல்", ar: "قائمة عناصر" },
  "blk.repeater": { en: "Repeater", ms: "Pengulang", zh: "重复字段", ta: "மீண்டும் செய்பவர்", ar: "مكرر" },
  "blk.repeaterHint": { en: "Repeatable rows", ms: "Baris boleh ulang", zh: "可重复行", ta: "மீண்டும் செய்யக்கூடிய வரிசைகள்", ar: "صفوف قابلة للتكرار" },
  "blk.image-choice": { en: "Image choice", ms: "Pilihan imej", zh: "图片选择", ta: "பட தேர்வு", ar: "اختيار صورة" },
  "blk.image-choiceHint": { en: "Choose from images", ms: "Pilih dari imej", zh: "从图片中选择", ta: "படங்களில் இருந்து தேர்ந்தெடுக்கவும்", ar: "اختر من الصور" },
  "blk.captcha": { en: "Captcha", ms: "Captcha", zh: "验证码", ta: "கேப்ட்சா", ar: "كابتشا" },
  "blk.captchaHint": { en: "Anti-spam check", ms: "Semakan anti-spam", zh: "反垃圾验证", ta: "ஸ்பேம் எதிர்ப்பு சரிபார்ப்பு", ar: "فحص مكافحة البريد العشوائي" },
  "blk.quantity": { en: "Quantity", ms: "Kuantiti", zh: "数量", ta: "அளவு", ar: "الكمية" },
  "blk.quantityHint": { en: "Quantity selector", ms: "Pemilih kuantiti", zh: "数量选择器", ta: "அளவு தேர்வி", ar: "محدد الكمية" },
  "blk.option": { en: "Option", ms: "Pilihan", zh: "选项", ta: "விருப்பம்", ar: "خيار" },
  "blk.optionHint": { en: "Paid add-on option", ms: "Pilihan tambahan berbayar", zh: "付费附加选项", ta: "கட்டண சேர்க்கை விருப்பம்", ar: "خيار إضافي مدفوع" },
  "blk.shipping": { en: "Shipping", ms: "Penghantaran", zh: "运费", ta: "ஷிப்பிங்", ar: "الشحن" },
  "blk.shippingHint": { en: "Shipping method", ms: "Kaedah penghantaran", zh: "配送方式", ta: "ஷிப்பிங் முறை", ar: "طريقة الشحن" },
  "blk.total": { en: "Total", ms: "Jumlah", zh: "合计", ta: "மொத்தம்", ar: "الإجمالي" },
  "blk.totalHint": { en: "Sale total display", ms: "Paparan jumlah jualan", zh: "销售总额显示", ta: "விற்பனை மொத்தக் காட்சி", ar: "عرض إجمالي البيع" },
  "bld.palette.standard": { en: "Standard Fields", ms: "Medan Standard", zh: "标准字段", ta: "நிலையான புலங்கள்", ar: "الحقول القياسية" },
  "bld.palette.advanced": { en: "Advanced Fields", ms: "Medan Lanjutan", zh: "高级字段", ta: "மேம்பட்ட புலங்கள்", ar: "الحقول المتقدمة" },
  "bld.palette.pricing": { en: "Pricing Fields", ms: "Medan Harga", zh: "定价字段", ta: "விலை புலங்கள்", ar: "حقول التسعير" },
  "bld.description": { en: "Description", ms: "Penerangan", zh: "描述", ta: "விளக்கம்", ar: "الوصف" },
  "bld.descriptionPh": { en: "Helper text shown under the field", ms: "Teks bantuan di bawah medan", zh: "字段下方显示的辅助文本", ta: "புலத்தின் கீழ் காட்டப்படும் உதவி உரை", ar: "نص مساعد يظهر أسفل الحقل" },
  "bld.placeholder": { en: "Placeholder", ms: "Placeholder", zh: "占位符", ta: "பிளேஸ்ஹோல்டர்", ar: "نص إرشادي" },
  "bld.placeholderPh": { en: "e.g. Enter your answer…", ms: "cth: Masukkan jawapan…", zh: "例如：输入您的答案…", ta: "எ.கா: உங்கள் பதிலை உள்ளிடவும்…", ar: "مثال: أدخل إجابتك…" },
  "bld.cssClass": { en: "CSS class", ms: "Kelas CSS", zh: "CSS 类", ta: "CSS வகுப்பு", ar: "فئة CSS" },
  "bld.cssClassPh": { en: "Optional custom class", ms: "Kelas tersuai pilihan", zh: "可选自定义类", ta: "விருப்ப தனிப்பயன் வகுப்பு", ar: "فئة مخصصة اختيارية" },
  "bld.advanced": { en: "Advanced", ms: "Lanjutan", zh: "高级", ta: "மேம்பட்டது", ar: "متقدم" },
  "bld.inlineStyle": { en: "Inline style (CSS)", ms: "Gaya sebaris (CSS)", zh: "内联样式（CSS）", ta: "இன்லைன் பாணி (CSS)", ar: "نمط مضمّن (CSS)" },
  "bld.inlineStylePh": { en: "e.g. color:#e11d48;font-weight:600", ms: "cth: color:#e11d48;font-weight:600", zh: "例如：color:#e11d48;font-weight:600", ta: "எ.கா: color:#e11d48;font-weight:600", ar: "مثال: color:#e11d48;font-weight:600" },
  "bld.inlineStyleHint": { en: "Safe CSS only. url() and background-image are stripped. Applied to this field's element.", ms: "CSS selamat sahaja — url()/background-image dibuang. Digunakan pada elemen medan ini.", zh: "仅限安全 CSS — url()/background-image 会被移除。应用于此字段的元素。", ta: "பாதுகாப்பான CSS மட்டும் — url()/background-image நீக்கப்படும். இந்த புலத்தின் உறுப்புக்கு பயன்படுத்தப்படும்.", ar: "CSS آمن فقط — تتم إزالة url()/background-image. يُطبق على عنصر هذا الحقل." },
  "bld.customJs": { en: "Custom JS (power users)", ms: "JS tersuai (kuasa pengguna)", zh: "自定义 JS（高级用户）", ta: "தனிப்பயன் JS (மேம்பட்ட பயனர்கள்)", ar: "JS مخصص (للمستخدمين المتقدمين)" },
  "bld.customJsWarning": { en: "This JavaScript runs on every buyer's page. Only the form owner can set it, and it is scanned for malicious code before it goes live. Use with care.", ms: "JavaScript ini berjalan pada halaman setiap pembeli. Hanya pemilik borang boleh menetapkannya, dan ia diimbas untuk kod berniat jahat sebelum live. Guna dengan berhati-hati.", zh: "此 JavaScript 会在每个买家的页面上运行。仅表单所有者可设置，上线前会扫描恶意代码。请谨慎使用。", ta: "இந்த JavaScript ஒவ்வொரு வாங்குபவரின் பக்கத்திலும் இயங்கும். படிவ உரிமையாளர் மட்டுமே அமைக்க முடியும், மேலும் இது நேரலையாகும் முன் தீங்கிழைக்கும் குறியீட்டிற்காக ஸ்கேன் செய்யப்படும். கவனமாகப் பயன்படுத்தவும்.", ar: "يعمل هذا الجافاسكربت على صفحة كل مشترٍ. يمكن لمالك النموذج فقط تعيينه، ويتم فحصه بحثاً عن تعليمات برمجية ضارة قبل نشره. استخدمه بحذر." },
  "bld.maxLength": { en: "Max length", ms: "Panjang maks", zh: "最大长度", ta: "அதிகபட்ச நீளம்", ar: "الحد الأقصى للطول" },
  "bld.defaultValue": { en: "Default value", ms: "Nilai lalai", zh: "默认值", ta: "இயல்புநிலை மதிப்பு", ar: "القيمة الافتراضية" },
  "bld.defaultValuePh": { en: "Pre-filled value", ms: "Nilai pra-isi", zh: "预填值", ta: "முன் நிரப்பப்பட்ட மதிப்பு", ar: "قيمة معبأة مسبقاً" },
  "bld.min": { en: "Min", ms: "Min", zh: "最小值", ta: "குறைந்தபட்சம்", ar: "الحد الأدنى" },
  "bld.max": { en: "Max", ms: "Maks", zh: "最大值", ta: "அதிகபட்சம்", ar: "الحد الأقصى" },
  "bld.step": { en: "Step", ms: "Langkah", zh: "步长", ta: "படி", ar: "الخطوة" },
  "bld.nameFormat": { en: "Name format", ms: "Format nama", zh: "姓名格式", ta: "பெயர் வடிவம்", ar: "تنسيق الاسم" },
  "bld.nameFull": { en: "Full name", ms: "Nama penuh", zh: "全名", ta: "முழு பெயர்", ar: "الاسم الكامل" },
  "bld.nameSimple": { en: "Simple (single field)", ms: "Ringkas (satu medan)", zh: "简单（单字段）", ta: "எளிய (ஒற்றை புலம்)", ar: "بسيط (حقل واحد)" },
  "bld.dateFormat": { en: "Format", ms: "Format", zh: "格式", ta: "வடிவம்", ar: "التنسيق" },
  "bld.fmtDate": { en: "Date", ms: "Tarikh", zh: "日期", ta: "தேதி", ar: "تاريخ" },
  "bld.fmtDateTime": { en: "Date & time", ms: "Tarikh & masa", zh: "日期和时间", ta: "தேதி & நேரம்", ar: "التاريخ والوقت" },
  "bld.fmtTime": { en: "Time", ms: "Masa", zh: "时间", ta: "நேரம்", ar: "الوقت" },
  "bld.choices": { en: "Choices", ms: "Pilihan", zh: "选项", ta: "தேர்வுகள்", ar: "الخيارات" },
  "bld.choiceLabel": { en: "Label", ms: "Label", zh: "标签", ta: "லேபிள்", ar: "التسمية" },
  "bld.choicePrice": { en: "Price ({cur})", ms: "Harga ({cur})", zh: "价格（{cur}）", ta: "விலை ({cur})", ar: "السعر ({cur})" },
  "bld.addChoice": { en: "Add choice", ms: "Tambah pilihan", zh: "添加选项", ta: "தேர்வு சேர்", ar: "إضافة خيار" },
  // Shipping rate source (managed zones). A shipping block either keeps its own
  // priced list (the default, unchanged) or charges the managed zone rate the
  // buyer's address resolves to.
  "bld.shipSource": { en: "Shipping rate source", ms: "Sumber kadar penghantaran", zh: "运费来源", ta: "ஷிப்பிங் கட்டண மூலம்", ar: "مصدر سعر الشحن" },
  "bld.shipSourceStatic": { en: "This form's own list", ms: "Senarai borang ini", zh: "本表单的列表", ta: "இந்தப் படிவத்தின் பட்டியல்", ar: "قائمة هذا النموذج" },
  "bld.shipSourceZones": { en: "Managed zones (by address)", ms: "Zon terurus (ikut alamat)", zh: "托管区域（按地址）", ta: "நிர்வகிக்கப்படும் மண்டலங்கள் (முகவரி வாரியாக)", ar: "مناطق مُدارة (حسب العنوان)" },
  "bld.shipSourceCourier": { en: "Courier quote (at checkout)", ms: "Sebut harga kurier (semasa bayar)", zh: "快递实时报价（结账时）", ta: "கூரியர் மதிப்பீடு (பணம் செலுத்தும்போது)", ar: "عرض سعر شركة شحن (عند الدفع)" },
  "bld.shipCourierProvider": { en: "Courier", ms: "Pembekal", zh: "快递服务商", ta: "கூரியர்", ar: "مزود الشحن" },
  "bld.shipCourierNone": { en: "Choose a courier", ms: "Pilih pembekal", zh: "选择快递服务商", ta: "கூரியரைத் தேர்ந்தெடுக்கவும்", ar: "اختر مزود الشحن" },
  "bld.shipCourierHint": { en: "The zone set below is the FALLBACK. The buyer sees its estimate, and it is charged when the courier quote fails, times out, or the courier is not connected.", ms: "Set zon di bawah ialah PELAN B: ia adalah anggaran yang dilihat pembeli, dan ia dikenakan jika sebut harga kurier gagal, tamat masa atau pembekal belum disambung. Sambungkan pembekal di Tetapan Borang → Penghantaran.", zh: "下方的区域组是备用方案：它是买家看到的预估金额，当快递报价失败、超时或未连接时会按此收费。请在「表单设置 → 运输」中连接快递。", ta: "கீழே உள்ள மண்டலத் தொகுப்பு மாற்று வழி: வாங்குபவர் காணும் மதிப்பீடு அதுவே; கூரியர் மதிப்பீடு தோல்வியடைந்தால், நேரம் முடிந்தால் அல்லது கூரியர் இணைக்கப்படவில்லை என்றால் அது வசூலிக்கப்படும். படிவ அமைப்புகள் → ஷிப்பிங் இல் கூரியரை இணைக்கவும்.", ar: "مجموعة المناطق أدناه هي البديل: هي التقدير الذي يراه المشتري، وتُفرض إذا فشل عرض سعر الشركة أو انتهت مهلته أو لم تكن الشركة متصلة. اربط الشركة من إعدادات النموذج ← الشحن." },
  "bld.shipZoneSet": { en: "Zone set", ms: "Set zon", zh: "区域组", ta: "மண்டல தொகுப்பு", ar: "مجموعة المناطق" },
  "bld.shipZoneSetNone": { en: "Choose a zone set", ms: "Pilih set zon", zh: "选择区域组", ta: "மண்டல தொகுப்பைத் தேர்ந்தெடுக்கவும்", ar: "اختر مجموعة مناطق" },
  "bld.shipNoZoneSets": { en: "No zone set yet. Create one in Form Settings → Shipping.", ms: "Belum ada set zon — cipta satu dalam Tetapan Borang → Penghantaran.", zh: "暂无区域组——请在表单设置 → 运输中创建。", ta: "இன்னும் மண்டல தொகுப்பு இல்லை — படிவ அமைப்புகள் → ஷிப்பிங் இல் உருவாக்கவும்.", ar: "لا توجد مجموعة مناطق بعد — أنشئ واحدة في إعدادات النموذج ← الشحن." },
  "bld.shipMapHint": { en: "Which of this form's fields carries the buyer's address:", ms: "Medan borang ini yang membawa alamat pembeli:", zh: "本表单中承载买家地址的字段：", ta: "வாங்குபவரின் முகவரியைக் கொண்டுள்ள இந்தப் படிவத்தின் புலம்:", ar: "حقل هذا النموذج الذي يحمل عنوان المشتري:" },
  "bld.shipStateField": { en: "State field", ms: "Medan negeri", zh: "州属字段", ta: "மாநில புலம்", ar: "حقل الولاية" },
  "bld.shipDistrictField": { en: "City / district field", ms: "Medan bandar / daerah", zh: "城市／地区字段", ta: "நகரம் / மாவட்ட புலம்", ar: "حقل المدينة / المنطقة" },
  "bld.shipPostcodeField": { en: "Postcode field", ms: "Medan poskod", zh: "邮编字段", ta: "அஞ்சல் குறியீடு புலம்", ar: "حقل الرمز البريدي" },
  "bld.shipNoMatch": { en: "If no zone matches", ms: "Jika tiada zon padan", zh: "若无区域匹配", ta: "எந்த மண்டலமும் பொருந்தவில்லை என்றால்", ar: "إذا لم تطابق أي منطقة" },
  "bld.shipNoMatchReject": { en: "Refuse the order", ms: "Tolak pesanan", zh: "拒绝订单", ta: "ஆர்டரை நிராகரி", ar: "ارفض الطلب" },
  "bld.shipNoMatchDefault": { en: "Use the fallback zone", ms: "Guna zon lalai", zh: "使用回退区域", ta: "இயல்புநிலை மண்டலத்தைப் பயன்படுத்து", ar: "استخدم المنطقة الاحتياطية" },
  "bld.shipWeight": { en: "Parcel weight (g)", ms: "Berat bungkusan (g)", zh: "包裹重量（g）", ta: "பார்சல் எடை (g)", ar: "وزن الشحنة (غ)" },
  "bld.shipFieldNone": { en: "Not collected on this form", ms: "Tidak dikumpul pada borang ini", zh: "本表单不收集", ta: "இந்தப் படிவத்தில் சேகரிக்கப்படவில்லை", ar: "غير مُجمَّع في هذا النموذج" },
  "bld.removeChoice": { en: "Remove choice", ms: "Buang pilihan", zh: "移除选项", ta: "தேர்வை நீக்கு", ar: "إزالة الخيار" },
  "bld.choiceOne": { en: "Option 1", ms: "Pilihan 1", zh: "选项 1", ta: "விருப்பம் 1", ar: "خيار 1" },
  "bld.choiceTwo": { en: "Option 2", ms: "Pilihan 2", zh: "选项 2", ta: "விருப்பம் 2", ar: "خيار 2" },
  "bld.consentDefault": { en: "I agree to the terms", ms: "Saya bersetuju dengan terma", zh: "我同意条款", ta: "விதிமுறைகளை ஏற்கிறேன்", ar: "أوافق على الشروط" },
  "bld.sectionDefault": { en: "Section title", ms: "Tajuk seksyen", zh: "分区标题", ta: "பிரிவு தலைப்பு", ar: "عنوان القسم" },
  "bld.totalDefault": { en: "Total", ms: "Jumlah", zh: "合计", ta: "மொத்தம்", ar: "الإجمالي" },
  "bld.captchaDefault": { en: "Enter the code shown", ms: "Masukkan kod yang dipaparkan", zh: "输入显示的验证码", ta: "காட்டப்படும் குறியீட்டை உள்ளிடவும்", ar: "أدخل الرمز المعروض" },
  "bld.requiredMsg": { en: "Required message", ms: "Mesej wajib", zh: "必填提示", ta: "கட்டாய செய்தி", ar: "رسالة مطلوبة" },
  "bld.requiredMsgPh": { en: "Custom error when empty", ms: "Ralat tersuai bila kosong", zh: "为空时的自定义错误", ta: "காலியாக இருக்கும்போது தனிப்பயன் பிழை", ar: "خطأ مخصص عند الفراغ" },
  "bld.pattern": { en: "Validation pattern", ms: "Corak pengesahan", zh: "验证模式", ta: "சரிபார்ப்பு வடிவம்", ar: "نمط التحقق" },
  "bld.patternPh": { en: "Regex, e.g. ^[0-9]{5}$", ms: "Regex, cth: ^[0-9]{5}$", zh: "正则，例如 ^[0-9]{5}$", ta: "Regex, எ.கா: ^[0-9]{5}$", ar: "تعبير نمطي، مثال: ^[0-9]{5}$" },
  "bld.defaultQty": { en: "Default quantity", ms: "Kuantiti lalai", zh: "默认数量", ta: "இயல்புநிலை அளவு", ar: "الكمية الافتراضية" },
  "bld.perProductQty": { en: "Per-product quantity", ms: "Kuantiti setiap produk", zh: "每件商品数量", ta: "ஒவ்வொரு பொருளுக்கும் அளவு", ar: "الكمية لكل منتج" },
  "bld.duplicate": { en: "Duplicate", ms: "Duplikat", zh: "复制", ta: "நகலெடு", ar: "تكرار" },
  // #652: the block card's ⋯ actions menu (holds Duplicate + Remove).
  "bld.blockMenu": { en: "More actions", ms: "Tindakan lain", zh: "更多操作", ta: "மேலும் செயல்கள்", ar: "مزيد من الإجراءات" },
  "bld.builderHelp": { en: "How the builder works", ms: "Cara pembina berfungsi", zh: "构建器使用说明", ta: "பில்டர் எவ்வாறு செயல்படுகிறது", ar: "كيف يعمل المُنشئ" },

  // form settings (issue #177)
  "fs.title": { en: "Form Settings", ms: "Tetapan Borang", zh: "表单设置", ta: "படிவ அமைப்புகள்", ar: "إعدادات النموذج" },
  "fs.subtitle": { en: "Everything except the fields: general, confirmation, restrictions, notifications and the CHIP feed for this form.", ms: "Semua tentang borang ini kecuali medannya — umum, pengesahan, sekatan, notifikasi dan feed CHIP per-borang.", zh: "除字段外的所有表单设置 — 常规、确认、限制、通知和每表单 CHIP 配置。", ta: "புலங்கள் தவிர இந்த படிவத்தைப் பற்றிய அனைத்தும் — பொது, உறுதிப்படுத்தல், கட்டுப்பாடுகள், அறிவிப்புகள் மற்றும் ஒவ்வொரு படிவ CHIP ஃபீட்.", ar: "كل ما يخص هذا النموذج عدا حقوله — عام، تأكيد، قيود، إشعارات وخلاصة CHIP لكل نموذج." },
  "fs.general": { en: "General", ms: "Umum", zh: "常规", ta: "பொது", ar: "عام" },
  "fs.confirmation": { en: "Confirmation", ms: "Pengesahan", zh: "确认", ta: "உறுதிப்படுத்தல்", ar: "التأكيد" },
  "fs.restrictions": { en: "Restrictions", ms: "Sekatan", zh: "限制", ta: "கட்டுப்பாடுகள்", ar: "القيود" },
  "fs.notifications": { en: "Notifications", ms: "Notifikasi", zh: "通知", ta: "அறிவிப்புகள்", ar: "الإشعارات" },
  "fs.chip": { en: "CHIP", ms: "CHIP", zh: "CHIP", ta: "CHIP", ar: "CHIP" },
  "fs.slug": { en: "Slug (URL)", ms: "Slug (URL)", zh: "Slug（链接）", ta: "Slug (URL)", ar: "Slug (الرابط)" },
  // The slug-rename rejection the merchant reads is `apiErr.formSlugInUse` /
  // `apiErr.formSlugInvalid` (beside apiErr.formSlugTaken), because the API
  // reports those refusals through the SHARED machine-error-code path
  // (api/internal/httpapi/errcode.go) and the console translates them with
  // lib/apiErr — there is exactly one mechanism and one set of keys.
  "fs.slugDocTitle": { en: "How form URLs and slug renames work", ms: "Cara URL borang dan penamaan semula slug berfungsi", zh: "表单网址与 slug 重命名说明", ta: "படிவ URL மற்றும் slug மறுபெயரிடல் எப்படி வேலை செய்கிறது", ar: "كيف تعمل روابط النموذج وإعادة تسمية الـ slug" },
  // Card t_8f5d6fb5: the brand colour now paints the public form's submit
  // button, its hover/pressed states and the checkbox/radio accent, and the
  // label colour is chosen so it stays readable on that colour.
  "fs.themeDocTitle": { en: "How your brand colour is applied to the public form", ms: "Cara warna jenama anda digunakan pada borang awam", zh: "品牌颜色如何应用到公开表单", ta: "உங்கள் பிராண்ட் நிறம் பொது படிவத்தில் எவ்வாறு பயன்படுத்தப்படுகிறது", ar: "كيف يُطبَّق لون علامتك التجارية على النموذج العام" },
  "fs.slugHint": { en: "The URL path this form is served at (a-z, 0-9, dash; 1-40 characters). Saving a change moves the form to the new URL. The old URL keeps working through a temporary redirect, and the slug must be free in this workspace.", ms: "Laluan URL borang ini (a-z, 0-9, sengkang; 1-40 aksara). Menyimpan perubahan memindahkan borang ke URL baharu — URL lama masih berfungsi melalui ubah hala sementara, dan slug mesti belum digunakan dalam workspace ini.", zh: "此表单的网址路径（a-z、0-9、连字符；1-40 个字符）。保存后会移动到新的网址——旧网址通过临时重定向继续可用，且该 slug 必须在当前工作区中未被占用。", ta: "இந்தப் படிவம் வழங்கப்படும் URL பாதை (a-z, 0-9, கோடு; 1-40 எழுத்துகள்). சேமித்தால் படிவம் புதிய URL-க்கு நகரும் — பழைய URL தற்காலிக திருப்பிவிடல் மூலம் தொடர்ந்து வேலை செய்யும், மேலும் slug இந்த பணியிடத்தில் காலியாக இருக்க வேண்டும்.", ar: "مسار الرابط الذي يُقدَّم عليه هذا النموذج (a-z و0-9 والشرطة؛ 1-40 حرفاً). حفظ التغيير ينقل النموذج إلى الرابط الجديد — ويبقى الرابط القديم يعمل عبر إعادة توجيه مؤقتة، ويجب أن يكون الـ slug متاحاً في مساحة العمل هذه." },
  "fs.save": { en: "Save", ms: "Simpan", zh: "保存", ta: "சேமி", ar: "حفظ" },
  "fs.saved": { en: "Saved ✓", ms: "Disimpan ✓", zh: "已保存 ✓", ta: "சேமிக்கப்பட்டது ✓", ar: "تم الحفظ ✓" },
  "fs.loading": { en: "Loading…", ms: "Memuat…", zh: "加载中…", ta: "ஏற்றுகிறது…", ar: "جارٍ التحميل…" },
  "fs.loadErr": { en: "Failed to load form settings.", ms: "Gagal memuatkan tetapan borang.", zh: "加载表单设置失败。", ta: "படிவ அமைப்புகளை ஏற்ற முடியவில்லை.", ar: "فشل تحميل إعدادات النموذج." },
  "fs.notFound": { en: "Form not found, or it's in a different workspace.", ms: "Borang tidak dijumpai, atau ia berada dalam ruang kerja lain.", zh: "未找到表单，或它位于其他工作区。", ta: "படிவம் காணப்படவில்லை, அல்லது அது வேறு பணியிடத்தில் உள்ளது.", ar: "لم يتم العثور على النموذج، أو أنه في مساحة عمل أخرى." },
  "fs.noForm": { en: "Select a form to edit its settings.", ms: "Pilih borang untuk edit tetapannya.", zh: "选择一个表单以编辑其设置。", ta: "அதன் அமைப்புகளைத் திருத்த ஒரு படிவத்தைத் தேர்ந்தெடுக்கவும்.", ar: "اختر نموذجاً لتحرير إعداداته." },
  "fs.editFields": { en: "Edit fields", ms: "Edit medan", zh: "编辑字段", ta: "புலங்களைத் திருத்து", ar: "تحرير الحقول" },
  // Issue #438: QR ordering flow — QR generation in form settings.
  "fs.qrTitle": { en: "QR code (table ordering)", ms: "Kod QR (pesanan meja)", zh: "二维码（桌边点餐）", ta: "QR குறியீடு (மேசை ஆர்டர்)", ar: "رمز QR (طلب الطاولة)" },
  "fs.qrHelp": { en: "Generate a QR that points to this form's public URL. Optionally enter a table number; it is encoded as ?table=N so the order reaches you with the table. Print it and put it on a table stand.", ms: "Jana QR yang menunjuk ke URL awam borang ini. Pilihan: masukkan nombor meja — ia dikodkan sebagai ?table=N supaya pesanan sampai kepada anda dengan meja. Cetak dan letak di stand meja.", zh: "生成指向此表单公开链接的二维码。可选输入桌号——编码为 ?table=N，订单会连同桌号送达。打印并放在桌牌上。", ta: "இந்த படிவத்தின் பொது URL-ஐ சுட்டிக்காட்டும் QR-ஐ உருவாக்கவும். விருப்பமாக மேசை எண்ணை உள்ளிடவும் — அது ?table=N என குறியாக்கம் செய்யப்படுகிறது, ஆர்டர் மேசையுடன் உங்களை அடைகிறது. அச்சிட்டு மேசை ஸ்டாண்டில் வைக்கவும்.", ar: "أنشئ رمز QR يشير إلى الرابط العام لهذا النموذج. أدخل رقم الطاولة اختيارياً — يُرمَّز كـ ?table=N ليصل الطلب إليك مع الطاولة. اطبعه وضعه على حامل الطاولة." },
  "fs.qrTablePh": { en: "Table no. (optional)", ms: "No. meja (pilihan)", zh: "桌号（可选）", ta: "மேசை எண் (விருப்பம்)", ar: "رقم الطاولة (اختياري)" },
  "fs.qrGenerate": { en: "Generate QR", ms: "Jana QR", zh: "生成二维码", ta: "QR உருவாக்கு", ar: "إنشاء رمز QR" },
  "fs.qrDownload": { en: "Download PNG", ms: "Muat turun PNG", zh: "下载 PNG", ta: "PNG பதிவிறக்கு", ar: "تنزيل PNG" },
  "fs.qrStand": { en: "Print stand", ms: "Cetak stand", zh: "打印立牌", ta: "ஸ்டாண்டை அச்சிடு", ar: "طباعة الحامل" },
  "fs.qrError": { en: "Failed to generate QR.", ms: "Gagal menjana QR.", zh: "生成二维码失败。", ta: "QR உருவாக்க முடியவில்லை.", ar: "فشل إنشاء رمز QR." },
  // Issue #471: product image size + layout (thumbnail-left / image-top menu card).
  // Issue #497: restaurant menu is a per-form setting (its own Form Settings tab).
  "fs.menu": { en: "Restaurant menu", ms: "Menu restoran", zh: "餐厅菜单", ta: "உணவக மெனு", ar: "قائمة المطعم" },
  "fs.menuDocs": { en: "Restaurant menu & basket guide", ms: "Panduan menu restoran & bakul", zh: "餐厅菜单与购物篮指南", ta: "உணவக மெனு & கூடை வழிகாட்டி", ar: "دليل قائمة المطعم والسلة" },
  "fs.menuHint": { en: "Turn this form into a QR-ordering restaurant menu. The menu is served at this form's own URL and lists only the products this form's product blocks reference.", ms: "Jadikan borang ini menu restoran pesanan QR. Menu dipaparkan di URL borang ini dan menyenaraikan hanya produk yang dirujuk oleh blok produk borang ini.", zh: "将此表单变成二维码点餐的餐厅菜单。菜单在此表单自身的网址上提供，且仅列出此表单商品区块所引用的商品。", ta: "இந்தப் படிவத்தை QR-ஆர்டர் உணவக மெனுவாக மாற்றவும். மெனு இந்தப் படிவத்தின் சொந்த URL-இல் வழங்கப்படும், இந்தப் படிவத்தின் பொருள் தொகுதிகள் குறிப்பிடும் பொருட்களை மட்டுமே பட்டியலிடும்.", ar: "حوّل هذا النموذج إلى قائمة مطعم للطلب عبر QR. تُقدَّم القائمة على رابط النموذج نفسه وتسرد فقط المنتجات التي تشير إليها كتل منتجات هذا النموذج." },
  "fs.menuNoProducts": { en: "This form has no product blocks yet. Add a Product field in the builder so the menu has items to show.", ms: "Borang ini belum ada blok produk — tambah medan Produk dalam pembina supaya menu ada item untuk dipaparkan.", zh: "此表单尚无商品区块——请在构建器中添加“商品”字段，让菜单有可显示的商品。", ta: "இந்தப் படிவத்தில் இன்னும் பொருள் தொகுதிகள் இல்லை — மெனுவில் காட்ட பொருட்கள் இருக்க பில்டரில் ஒரு பொருள் புலத்தைச் சேர்க்கவும்.", ar: "لا يحتوي هذا النموذج على كتل منتجات بعد — أضف حقل منتج في المنشئ لتظهر عناصر في القائمة." },
  "fs.menuProductsAvailable": { en: "Products this form references", ms: "Produk yang dirujuk borang ini", zh: "此表单引用的商品", ta: "இந்தப் படிவம் குறிப்பிடும் பொருட்கள்", ar: "المنتجات التي يشير إليها هذا النموذج" },
  "fs.menuSaved": { en: "Menu saved", ms: "Menu disimpan", zh: "菜单已保存", ta: "மெனு சேமிக்கப்பட்டது", ar: "تم حفظ القائمة" },
  "fs.prodImgSize": { en: "Product image size", ms: "Saiz imej produk", zh: "产品图片尺寸", ta: "தயாரிப்பு பட அளவு", ar: "حجم صورة المنتج" },
  "fs.prodImgSizeSmall": { en: "Small (52px)", ms: "Kecil (52px)", zh: "小（52px）", ta: "சிறியது (52px)", ar: "صغير (52px)" },
  "fs.prodImgSizeMedium": { en: "Medium (96px)", ms: "Sederhana (96px)", zh: "中（96px）", ta: "நடுத்தரம் (96px)", ar: "متوسط (96px)" },
  "fs.prodImgSizeLarge": { en: "Large (160px)", ms: "Besar (160px)", zh: "大（160px）", ta: "பெரியது (160px)", ar: "كبير (160px)" },
  "fs.prodImgSizeHidden": { en: "Hidden", ms: "Tersembunyi", zh: "隐藏", ta: "மறை", ar: "مخفي" },
  "fs.prodImgLayout": { en: "Product image layout", ms: "Susun atur imej produk", zh: "产品图片布局", ta: "தயாரிப்பு பட அமைப்பு", ar: "تخطيط صورة المنتج" },
  "fs.prodImgLayoutLeft": { en: "Thumbnail left", ms: "Imej di kiri", zh: "缩略图在左", ta: "இடதுபுறம் சிறுபடம்", ar: "مصغّرة على اليسار" },
  "fs.prodImgLayoutTop": { en: "Image top (menu card)", ms: "Imej di atas (kad menu)", zh: "图片在上（菜单卡片）", ta: "படம் மேலே (மெனு அட்டை)", ar: "الصورة أعلى (بطاقة القائمة)" },
  "fs.prodImgPreview": { en: "Preview", ms: "Pratonton", zh: "预览", ta: "முன்னோட்டம்", ar: "معاينة" },
  "fs.prodImgSample": { en: "Sample product", ms: "Produk contoh", zh: "示例产品", ta: "மாதிரி தயாரிப்பு", ar: "منتج تجريبي" },
  "fs.confirmationHint": { en: "What buyers see after submitting. Leave blank to use the default thank-you page.", ms: "Apa yang pembeli lihat selepas hantar. Kosongkan untuk guna halaman terima kasih lalai.", zh: "买家提交后看到的内容。留空则使用默认感谢页。", ta: "சமர்ப்பித்த பிறகு வாங்குபவர்கள் பார்ப்பது. இயல்புநிலை நன்றி பக்கத்தைப் பயன்படுத்த காலியாக விடவும்.", ar: "ما يراه المشترون بعد الإرسال. اتركه فارغاً لاستخدام صفحة الشكر الافتراضية." },
  // Issue #393: per-form payment-failed message.
  "fs.failureMessage": { en: "Payment failed message (HTML, sanitized)", ms: "Mesej pembayaran gagal (HTML, disanitasi)", zh: "支付失败消息（HTML，已净化）", ta: "கட்டணம் தோல்வி செய்தி (HTML, சுத்திகரிக்கப்பட்டது)", ar: "رسالة فشل الدفع (HTML، منقّاة)" },
  "fs.failureMessageHint": { en: "Shown to the buyer when a payment fails. Leave blank to use the default. The failure redirect (if set) takes precedence.", ms: "Dipaparkan kepada pembeli bila pembayaran gagal. Kosongkan untuk guna lalai. Redirect gagal (jika ditetapkan) diutamakan.", zh: "支付失败时向买家显示。留空则使用默认值。失败跳转（若设置）优先。", ta: "கட்டணம் தோல்வியடையும் போது வாங்குபவருக்குக் காட்டப்படும். இயல்புநிலையைப் பயன்படுத்த காலியாக விடவும். தோல்வி திருப்புதல் (அமைத்தால்) முன்னுரிமை.", ar: "يُعرض للمشتري عند فشل الدفع. اتركه فارغاً لاستخدام الافتراضي. توجيه الفشل (إن تم ضبطه) له الأولوية." },
  // Issue #179: per-form confirmations (Gravity-Forms-style).
  "fs.confirmations": { en: "Confirmations", ms: "Pengesahan", zh: "确认", ta: "உறுதிப்படுத்தல்கள்", ar: "التأكيدات" },
  // Card t_1463b71e: the Confirmation tab had two Save buttons with the same
  // label over two different writes. Each now names the write it performs.
  "fs.saveConfirmationSettings": { en: "Save confirmation settings", ms: "Simpan tetapan pengesahan", zh: "保存确认设置", ta: "உறுதிப்படுத்தல் அமைப்புகளைச் சேமி", ar: "حفظ إعدادات التأكيد" },
  "fs.saveConfirmations": { en: "Save confirmations", ms: "Simpan pengesahan", zh: "保存确认", ta: "உறுதிப்படுத்தல்களைச் சேமி", ar: "حفظ التأكيدات" },
  "fs.confirmationsHint": { en: "What buyers see after submitting. The first matching confirmation (by conditional logic) is shown; the default always matches.", ms: "Apa yang pembeli lihat selepas hantar. Pengesahan pertama yang padan (ikut logik bersyarat) dipaparkan; lalai sentiasa padan.", zh: "买家提交后看到的内容。显示第一个匹配的确认（按条件逻辑）；默认始终匹配。", ta: "சமர்ப்பித்த பிறகு வாங்குபவர்கள் பார்ப்பது. முதல் பொருந்தும் உறுதிப்படுத்தல் (நிபந்தனை தர்க்கத்தால்) காட்டப்படும்; இயல்புநிலை எப்போதும் பொருந்தும்.", ar: "ما يراه المشترون بعد الإرسال. يُعرض أول تأكيد مطابق (حسب المنطق الشرطي)؛ الافتراضي يطابق دائماً." },
  "fs.confName": { en: "Confirmation name", ms: "Nama pengesahan", zh: "确认名称", ta: "உறுதிப்படுத்தல் பெயர்", ar: "اسم التأكيد" },
  "fs.confDefault": { en: "Default", ms: "Lalai", zh: "默认", ta: "இயல்புநிலை", ar: "افتراضي" },
  "fs.confDelete": { en: "Delete", ms: "Padam", zh: "删除", ta: "நீக்கு", ar: "حذف" },
  "fs.confMessage": { en: "Message", ms: "Mesej", zh: "消息", ta: "செய்தி", ar: "رسالة" },
  "fs.confRedirect": { en: "Redirect", ms: "Redirect", zh: "重定向", ta: "திருப்பிவிடு", ar: "إعادة توجيه" },
  "fs.confMessageBody": { en: "Message (HTML, sanitized)", ms: "Mesej (HTML, disanitasi)", zh: "消息（HTML，已净化）", ta: "செய்தி (HTML, சுத்திகரிக்கப்பட்டது)", ar: "الرسالة (HTML، منقّاة)" },
  // Card t_0da81d94: the confirmation / failure bodies are built in the BUYER's
  // language when the merchant leaves them blank (the dictionary's
  // ResponseReceived / PaymentFailedBody), so the field says what blank means
  // instead of showing a sentence in one language to every merchant.
  "fs.builtInDefaultPlaceholder": { en: "Built-in default (shown in the form's language)", ms: "Lalai terbina (dipaparkan dalam bahasa borang)", zh: "内置默认（按表单语言显示）", ta: "உள்ளமைந்த இயல்புநிலை (படிவத்தின் மொழியில் காட்டப்படும்)", ar: "الافتراضي المضمّن (يُعرض بلغة النموذج)" },
  "fs.confRedirectUrl": { en: "Redirect URL (https://)", ms: "URL redirect (https://)", zh: "重定向链接（https://）", ta: "திருப்பு URL (https://)", ar: "رابط إعادة التوجيه (https://)" },
  "fs.confConditional": { en: "Enable conditional logic", ms: "Aktifkan logik bersyarat", zh: "启用条件逻辑", ta: "நிபந்தனை தர்க்கத்தை இயக்கு", ar: "تفعيل المنطق الشرطي" },
  "fs.confFieldId": { en: "Field ID", ms: "ID medan", zh: "字段 ID", ta: "புல ID", ar: "معرف الحقل" },
  "fs.confValue": { en: "Value", ms: "Nilai", zh: "值", ta: "மதிப்பு", ar: "القيمة" },
  "fs.confAdd": { en: "+ Add confirmation", ms: "+ Tambah pengesahan", zh: "+ 添加确认", ta: "+ உறுதிப்படுத்தலைச் சேர்", ar: "+ إضافة تأكيد" },
  "fs.opEquals": { en: "equals", ms: "sama dengan", zh: "等于", ta: "சமம்", ar: "يساوي" },
  "fs.opNotEquals": { en: "not equals", ms: "tidak sama", zh: "不等于", ta: "சமமல்ல", ar: "لا يساوي" },
  "fs.opContains": { en: "contains", ms: "mengandungi", zh: "包含", ta: "கொண்டுள்ளது", ar: "يحتوي" },
  "fs.opGt": { en: "greater than", ms: "lebih besar", zh: "大于", ta: "அதிகம்", ar: "أكبر من" },
  "fs.opLt": { en: "less than", ms: "kurang", zh: "小于", ta: "குறைவு", ar: "أقل من" },
  "fs.opNotEmpty": { en: "is not empty", ms: "tidak kosong", zh: "不为空", ta: "காலியாக இல்லை", ar: "غير فارغ" },
  // Issue #392: conditional-logic picker (field dropdown + context-aware value + summary).
  "fs.condIf": { en: "If", ms: "Jika", zh: "如果", ta: "என்றால்", ar: "إذا" },
  "fs.condSelectField": { en: "Select a field…", ms: "Pilih medan…", zh: "选择字段…", ta: "புலத்தைத் தேர்ந்தெடுக்கவும்…", ar: "اختر حقلاً…" },
  // Accessible names for the three controls the condition picker renders (they
  // carry no visible label of their own — the "If [field] [operator] [value]"
  // line is a summary, not a label). Card t_16ef9793: unnamed `<select>`s here
  // were 3 of the 4 critical axe `select-name` nodes reachable from
  // /form-settings' Notifications tab.
  "fs.condFieldName": { en: "Condition field", ms: "Medan syarat", zh: "条件字段", ta: "நிபந்தனை புலம்", ar: "حقل الشرط" },
  "fs.condOpName": { en: "Condition operator", ms: "Operator syarat", zh: "条件运算符", ta: "நிபந்தனை ஆபரேட்டர்", ar: "مشغل الشرط" },
  "fs.condValueName": { en: "Condition value", ms: "Nilai syarat", zh: "条件值", ta: "நிபந்தனை மதிப்பு", ar: "قيمة الشرط" },
  "fs.notificationsHint": { en: "Per-form receipt email. Leave subject and body blank to use the workspace defaults.", ms: "Emel resit per-borang. Kosongkan subjek dan badan untuk guna lalai ruang kerja.", zh: "每表单收据邮件。主题和正文留空则使用工作区默认值。", ta: "ஒவ்வொரு படிவ ரசீது மின்னஞ்சல். பணியிடத்தின் இயல்புநிலைகளைப் பயன்படுத்த தலைப்பு மற்றும் உடலை காலியாக விடவும்.", ar: "بريد إيصال لكل نموذج. اترك الموضوع والمحتوى فارغين لاستخدام افتراضيات مساحة العمل." },
  "fs.blankUsesWorkspace": { en: "(blank = use workspace default)", ms: "(kosong = guna lalai ruang kerja)", zh: "（留空 = 使用工作区默认值）", ta: "(காலியாக = பணியிடத்தின் இயல்புநிலையைப் பயன்படுத்து)", ar: "(فارغ = استخدم افتراضي مساحة العمل)" },

  // Issue #453: per-form email settings override of the workspace settings.
  "fs.emelTitle": { en: "Receipt & notification email (override)", ms: "Emel resit & notifikasi (override)", zh: "收据与通知邮件（覆盖）", ta: "ரசீது & அறிவிப்பு மின்னஞ்சல் (மேலெழுது)", ar: "بريد الإيصال والإشعار (تجاوز)" },
  "fs.emelHint": { en: "Override the workspace email settings for this form only. Leave a field on \"use workspace default\" to inherit it. Resolution order: this form → workspace → built-in default.", ms: "Override tetapan emel workspace untuk borang ini sahaja. Biarkan medan pada \"guna lalai workspace\" untuk mewarisinya. Urutan resolusi: borang ini → workspace → lalai terbina.", zh: "仅为该表单覆盖工作区邮件设置。将字段保留为“使用工作区默认值”即可继承。解析顺序：此表单 → 工作区 → 内置默认值。", ta: "இந்தப் படிவத்திற்கு மட்டும் பணியிட மின்னஞ்சல் அமைப்புகளை மேலெழுதவும். ஒரு புலத்தை \"பணியிட இயல்புநிலையைப் பயன்படுத்து\" என விட்டால் அது மரபுரிமையாகும். தீர்மான வரிசை: இந்தப் படிவம் → பணியிடம் → உள்ளமை இயல்புநிலை.", ar: "تجاوز إعدادات بريد مساحة العمل لهذا النموذج فقط. اترك الحقل على \"استخدام افتراضي مساحة العمل\" لوراثته. ترتيب الحل: هذا النموذج → مساحة العمل → الافتراضي المدمج." },
  "fs.emelSubject": { en: "Subject", ms: "Subjek", zh: "主题", ta: "பொருள்", ar: "الموضوع" },
  "fs.emelBody": { en: "Body (template)", ms: "Badan (templat)", zh: "正文（模板）", ta: "உடல் (வார்ப்புரு)", ar: "المحتوى (قالب)" },
  "fs.emelPlaceholders": { en: "Placeholders: {{ref_code}} {{amount}} {{items}} {{buyer_name}} {{form_url}} {{date}}.", ms: "Placeholder: {{ref_code}} {{amount}} {{items}} {{buyer_name}} {{form_url}} {{date}}.", zh: "占位符：{{ref_code}} {{amount}} {{items}} {{buyer_name}} {{form_url}} {{date}}。", ta: "பிளேஸ்ஹோல்டர்கள்: {{ref_code}} {{amount}} {{items}} {{buyer_name}} {{form_url}} {{date}}.", ar: "الرموز: {{ref_code}} {{amount}} {{items}} {{buyer_name}} {{form_url}} {{date}}." },
  "fs.emelReceiptEnabled": { en: "Send receipt to buyer", ms: "Hantar resit kepada pembeli", zh: "向买家发送收据", ta: "வாங்குபவருக்கு ரசீது அனுப்பு", ar: "إرسال الإيصال إلى المشتري" },
  "fs.emelNotifyMerchant": { en: "Notify me of each new sale", ms: "Maklumkan saya setiap jualan baharu", zh: "每笔新销售通知我", ta: "ஒவ்வொரு புதிய விற்பனைக்கும் எனக்கு அறிவி", ar: "أبلغني بكل عملية بيع جديدة" },
  "fs.emelHtml": { en: "HTML body (auto-escaped)", ms: "Badan HTML (auto-escape)", zh: "HTML 正文（自动转义）", ta: "HTML உடல் (தானியங்கி எஸ்கேப்)", ar: "محتوى HTML (تلقائي الترميز)" },
  "fs.emelMerchantEmail": { en: "Notification email", ms: "Emel notifikasi", zh: "通知邮箱", ta: "அறிவிப்பு மின்னஞ்சல்", ar: "بريد الإشعار" },
  "fs.emelAccountEmail": { en: "Account email", ms: "Emel akaun", zh: "账户邮箱", ta: "கணக்கு மின்னஞ்சல்", ar: "بريد الحساب" },
  "fs.emelInheritHint": { en: "(blank = use workspace default)", ms: "(kosong = guna lalai workspace)", zh: "（留空 = 使用工作区默认值）", ta: "(காலியாக = பணியிட இயல்புநிலையைப் பயன்படுத்து)", ar: "(فارغ = استخدم افتراضي مساحة العمل)" },
  "fs.emelWorkspaceDefault": { en: "Use workspace default", ms: "Guna lalai workspace", zh: "使用工作区默认值", ta: "பணியிட இயல்புநிலையைப் பயன்படுத்து", ar: "استخدام افتراضي مساحة العمل" },
  "fs.emelOn": { en: "On", ms: "Hidup", zh: "开启", ta: "இயக்கு", ar: "تشغيل" },
  "fs.emelOff": { en: "Off", ms: "Mati", zh: "关闭", ta: "அணை", ar: "إيقاف" },
  "fs.emelEffective": { en: "Effective (resolved) settings", ms: "Tetapan efektif (selepas resolusi)", zh: "有效（已解析）设置", ta: "பயனுள்ள (தீர்க்கப்பட்ட) அமைப்புகள்", ar: "الإعدادات الفعلية (المحلولة)" },
  "fs.emelNotifyShort": { en: "Merchant notify", ms: "Notifikasi merchant", zh: "商户通知", ta: "வணிகர் அறிவிப்பு", ar: "إشعار التاجر" },
  "fs.emelSave": { en: "Save email override", ms: "Simpan override emel", zh: "保存邮件覆盖", ta: "மின்னஞ்சல் மேலெழுதலைச் சேமி", ar: "حفظ تجاوز البريد" },
  "fs.emelSaved": { en: "Email override saved ✓", ms: "Override emel disimpan ✓", zh: "邮件覆盖已保存 ✓", ta: "மின்னஞ்சல் மேலெழுதல் சேமிக்கப்பட்டது ✓", ar: "تم حفظ تجاوز البريد ✓" },
  "fs.emelCleared": { en: "Overrides cleared. Inheriting the workspace settings ✓", ms: "Override dipadam — kini mewarisi tetapan workspace ✓", zh: "已清除覆盖 — 现继承工作区设置 ✓", ta: "மேலெழுதல்கள் அழிக்கப்பட்டன — பணியிட அமைப்புகளை மரபுரிமையாகப் பெறுகிறது ✓", ar: "تم مسح التجاوزات — يرث إعدادات مساحة العمل ✓" },
  "fs.emelClear": { en: "Clear overrides (use workspace)", ms: "Padam override (guna workspace)", zh: "清除覆盖（使用工作区）", ta: "மேலெழுதல்களை அழி (பணியிடத்தைப் பயன்படுத்து)", ar: "مسح التجاوزات (استخدام مساحة العمل)" },
  "fs.emelFail": { en: "Failed to save email settings.", ms: "Gagal menyimpan tetapan emel.", zh: "保存邮件设置失败。", ta: "மின்னஞ்சல் அமைப்புகளைச் சேமிக்க முடியவில்லை.", ar: "فشل حفظ إعدادات البريد." },
  // Issue #193: per-form notification rules (Gravity-Forms-style).
  "fs.notifHint": { en: "Email notifications this form sends. A default notification (notify you on payment) is pre-created. Edit it or add more. A form with no notifications sends no email.", ms: "Notifikasi emel yang dihantar borang ini. Notifikasi lalai (maklumkan anda selepas bayaran) disediakan — edit atau tambah lagi. Borang tanpa notifikasi tidak menghantar emel.", zh: "此表单发送的邮件通知。默认通知（付款时通知您）已预建 — 可编辑或添加更多。无通知的表单不发送邮件。", ta: "இந்த படிவம் அனுப்பும் மின்னஞ்சல் அறிவிப்புகள். இயல்புநிலை அறிவிப்பு (கட்டணத்தில் உங்களுக்கு அறிவிக்கும்) முன் உருவாக்கப்பட்டது — திருத்தவும் அல்லது மேலும் சேர்க்கவும். அறிவிப்புகள் இல்லாத படிவம் மின்னஞ்சல் அனுப்பாது.", ar: "إشعارات البريد التي يرسلها هذا النموذج. يتم إنشاء إشعار افتراضي (إشعارك عند الدفع) مسبقاً — حرّره أو أضف المزيد. النموذج بدون إشعارات لا يرسل بريداً." },
  "fs.notifAdd": { en: "Add notification", ms: "Tambah notifikasi", zh: "添加通知", ta: "அறிவிப்பு சேர்", ar: "إضافة إشعار" },
  "fs.notifName": { en: "Name", ms: "Nama", zh: "名称", ta: "பெயர்", ar: "الاسم" },
  "fs.notifEvent": { en: "Event", ms: "Acara", zh: "事件", ta: "நிகழ்வு", ar: "الحدث" },
  "fs.notifTo": { en: "Send to", ms: "Hantar kepada", zh: "发送至", ta: "அனுப்பு", ar: "إرسال إلى" },
  "fs.notifToHint": { en: "Email address, {admin_email} (your account email), or {field_id} (a submitted field value).", ms: "Alamat emel, {admin_email} (emel akaun anda), atau {field_id} (nilai medan yang dihantar).", zh: "邮箱地址、{admin_email}（您的账户邮箱）或 {field_id}（提交的字段值）。", ta: "மின்னஞ்சல் முகவரி, {admin_email} (உங்கள் கணக்கு மின்னஞ்சல்), அல்லது {field_id} (சமர்ப்பிக்கப்பட்ட புல மதிப்பு).", ar: "عنوان بريد، أو {admin_email} (بريد حسابك)، أو {field_id} (قيمة حقل مُرسل)." },
  "fs.notifSubject": { en: "Subject", ms: "Subjek", zh: "主题", ta: "பொருள்", ar: "الموضوع" },
  // The empty-field suggestion, in the merchant's language (t_541a31d5): the
  // Malay sample used to be hardcoded here on every locale.
  "fs.notifSubjectPh": { en: "New sale {{ref_code}}", ms: "Jualan baharu — {{ref_code}}", zh: "新销售 — {{ref_code}}", ta: "புதிய விற்பனை — {{ref_code}}", ar: "بيع جديد — {{ref_code}}" },
  // The response-mode flavour. A response form never records a sale, so the sale
  // suggestion would be wrong on the rule that fires when someone responds.
  // Kept in step with emailtext's NotificationResponse subjects.
  "fs.notifSubjectPhResponse": { en: "New submission received", ms: "Pendaftaran baharu diterima", zh: "收到新的提交", ta: "புதிய சமர்ப்பிப்பு பெறப்பட்டது", ar: "تم استلام إرسال جديد" },
  "fs.notifBody": { en: "Body (template)", ms: "Badan (templat)", zh: "正文（模板）", ta: "உடல் (வார்ப்புரு)", ar: "المحتوى (قالب)" },
  // An empty body is not "no body": it means JomForm's own message in the form's
  // language (see fs.notifSubjectPh and /docs/customize#language). The hint says
  // so without duplicating the template itself into the web bundle.
  "fs.notifBodyPh": { en: "Leave blank to send JomForm's own message, in the form's language.", ms: "Biarkan kosong untuk menghantar mesej JomForm sendiri, dalam bahasa borang.", zh: "留空即发送 JomForm 自带的讯息，语言跟随表单。", ta: "காலியாக விட்டால் JomForm இன் சொந்த செய்தி, படிவத்தின் மொழியில் அனுப்பப்படும்.", ar: "اتركه فارغاً لإرسال رسالة JomForm الخاصة، بلغة النموذج." },
  "fs.notifPlaceholders": { en: "Placeholders: {{ref_code}} {{amount}} {{items}} {{buyer_name}} {{form_url}} {{date}} {{full_name}} {{email}} or any {{field_id}}. A response form has no sale, so its sale placeholders render empty.", ms: "Placeholder: {{ref_code}} {{amount}} {{items}} {{buyer_name}} {{form_url}} {{date}} {{full_name}} {{email}} atau mana-mana {{field_id}}. Borang respons tiada jualan, jadi placeholder jualannya keluar kosong.", zh: "占位符：{{ref_code}} {{amount}} {{items}} {{buyer_name}} {{form_url}} {{date}} {{full_name}} {{email}} 或任意 {{field_id}}。回复表单没有销售，其销售占位符会输出为空。", ta: "பிளேஸ்ஹோல்டர்கள்: {{ref_code}} {{amount}} {{items}} {{buyer_name}} {{form_url}} {{date}} {{full_name}} {{email}} அல்லது எந்த {{field_id}}. பதில் படிவத்தில் விற்பனை இல்லை, எனவே அதன் விற்பனை பிளேஸ்ஹோல்டர்கள் காலியாக வெளியாகும்.", ar: "الرموز: {{ref_code}} {{amount}} {{items}} {{buyer_name}} {{form_url}} {{date}} {{full_name}} {{email}} أو أي {{field_id}}. نموذج الرد بلا عملية بيع، لذا تظهر رموز البيع فارغة." },
  // issue #395: inline "?" doc link next to the notification placeholder fields
  "fs.notifDocs": { en: "Placeholders reference", ms: "Rujukan placeholder", zh: "占位符参考", ta: "பிளேஸ்ஹோல்டர் குறிப்பு", ar: "مرجع الرموز" },
  "fs.notifCondition": { en: "Conditional logic (optional)", ms: "Logik bersyarat (pilihan)", zh: "条件逻辑（可选）", ta: "நிபந்தனை தர்க்கம் (விருப்பம்)", ar: "المنطق الشرطي (اختياري)" },
  "fs.notifCondField": { en: "Field", ms: "Medan", zh: "字段", ta: "புலம்", ar: "الحقل" },
  "fs.notifCondOp": { en: "Operator", ms: "Operator", zh: "运算符", ta: "ஆபரேட்டர்", ar: "المشغل" },
  "fs.notifCondValue": { en: "Value", ms: "Nilai", zh: "值", ta: "மதிப்பு", ar: "القيمة" },
  "fs.notifCondNone": { en: "No condition. Always send on this event", ms: "Tiada syarat — sentiasa hantar pada acara ini", zh: "无条件 — 始终在此事件时发送", ta: "நிபந்தனை இல்லை — இந்த நிகழ்வில் எப்போதும் அனுப்பு", ar: "بدون شرط — أرسل دائماً عند هذا الحدث" },
  "fs.notifEnabled": { en: "Enabled", ms: "Aktif", zh: "已启用", ta: "இயக்கப்பட்டது", ar: "مفعل" },
  "fs.notifDefault": { en: "Default", ms: "Lalai", zh: "默认", ta: "இயல்புநிலை", ar: "افتراضي" },
  "fs.notifDelete": { en: "Delete", ms: "Padam", zh: "删除", ta: "நீக்கு", ar: "حذف" },
  "fs.notifSave": { en: "Save notification", ms: "Simpan notifikasi", zh: "保存通知", ta: "அறிவிப்பைச் சேமி", ar: "حفظ الإشعار" },
  "fs.notifCancel": { en: "Cancel", ms: "Batal", zh: "取消", ta: "ரத்து", ar: "إلغاء" },
  "fs.notifEdit": { en: "Edit", ms: "Edit", zh: "编辑", ta: "திருத்து", ar: "تحرير" },
  "fs.notifEmpty": { en: "No notifications. This form sends no email.", ms: "Tiada notifikasi — borang ini tidak menghantar emel.", zh: "无通知 — 此表单不发送邮件。", ta: "அறிவிப்புகள் இல்லை — இந்த படிவம் மின்னஞ்சல் அனுப்பாது.", ar: "لا توجد إشعارات — هذا النموذج لا يرسل بريداً." },
  "fs.notifSaved": { en: "Notification saved ✓", ms: "Notifikasi disimpan ✓", zh: "通知已保存 ✓", ta: "அறிவிப்பு சேமிக்கப்பட்டது ✓", ar: "تم حفظ الإشعار ✓" },
  "fs.notifDeleted": { en: "Notification deleted", ms: "Notifikasi dipadam", zh: "通知已删除", ta: "அறிவிப்பு நீக்கப்பட்டது", ar: "تم حذف الإشعار" },
  "fs.notifFail": { en: "Failed to save notification", ms: "Gagal simpan notifikasi", zh: "保存通知失败", ta: "அறிவிப்பைச் சேமிக்க முடியவில்லை", ar: "فشل حفظ الإشعار" },
  "fs.notifEventSalePaid": { en: "Payment received", ms: "Bayaran diterima", zh: "收到付款", ta: "கட்டணம் பெறப்பட்டது", ar: "تم استلام الدفع" },
  "fs.notifEventSaleFailed": { en: "Payment failed", ms: "Bayaran gagal", zh: "付款失败", ta: "கட்டணம் தோல்வி", ar: "فشل الدفع" },
  "fs.notifEventResponseSubmitted": { en: "Response submitted", ms: "Respons dihantar", zh: "已提交回复", ta: "பதில் சமர்ப்பிக்கப்பட்டது", ar: "تم إرسال الرد" },
  "fs.notifEventSubscriptionCharged": { en: "Subscription charged", ms: "Langganan dicaj", zh: "订阅已扣费", ta: "சந்தா வசூலிக்கப்பட்டது", ar: "تم تحصيل الاشتراك" },
  "fs.notifOpEquals": { en: "is", ms: "ialah", zh: "等于", ta: "சமம்", ar: "يساوي" },
  "fs.notifOpNotEquals": { en: "is not", ms: "bukan", zh: "不等于", ta: "சமமல்ல", ar: "لا يساوي" },
  "fs.notifOpContains": { en: "contains", ms: "mengandungi", zh: "包含", ta: "கொண்டுள்ளது", ar: "يحتوي" },
  "fs.notifOpGt": { en: "greater than", ms: "lebih besar", zh: "大于", ta: "அதிகம்", ar: "أكبر من" },
  "fs.notifOpLt": { en: "less than", ms: "lebih kecil", zh: "小于", ta: "குறைவு", ar: "أقل من" },
  "fs.notifOpNotEmpty": { en: "is not empty", ms: "tidak kosong", zh: "不为空", ta: "காலியாக இல்லை", ar: "غير فارغ" },
  "fs.notifCondRemove": { en: "Remove condition", ms: "Buang syarat", zh: "移除条件", ta: "நிபந்தனையை நீக்கு", ar: "إزالة الشرط" },
  "fs.notifCondAdd": { en: "Add condition", ms: "Tambah syarat", zh: "添加条件", ta: "நிபந்தனை சேர்", ar: "إضافة شرط" },
  "fs.chipHint": { en: "Per-form CHIP credentials (its own CHIP feed). When set, this form uses these keys instead of the account defaults. Leave blank to use the account-level CHIP settings in Workspace settings.", ms: "Kredensial CHIP per-borang (feed CHIP tersendiri). Bila set, borang ini guna kunci ini, bukan lalai akaun. Kosongkan untuk guna tetapan CHIP peringkat akaun dalam Tetapan Workspace.", zh: "每表单 CHIP 凭证（自带的 CHIP 数据源）。设置后，此表单使用这些密钥而非账户默认值。留空则使用 Workspace 设置中的账户级 CHIP 设置。", ta: "ஒவ்வொரு படிவ CHIP சான்றுகள் (அதன் சொந்த CHIP ஊட்டம்). அமைத்தால், இந்த படிவம் கணக்கு இயல்புநிலைகளுக்குப் பதிலாக இந்த விசைகளைப் பயன்படுத்தும். Workspace அமைப்புகளில் உள்ள கணக்கு-நிலை CHIP அமைப்புகளைப் பயன்படுத்த காலியாக விடவும்.", ar: "بيانات CHIP لكل نموذج (تغذية CHIP خاصة بها). عند ضبطها، يستخدم هذا النموذج هذه المفاتيح بدلاً من افتراضيات الحساب. اتركه فارغاً لاستخدام إعدادات CHIP على مستوى الحساب في إعدادات المساحة." },
  "fs.chipConfigured": { en: "✓ Configured", ms: "✓ Dikonfigurasi", zh: "✓ 已配置", ta: "✓ கட்டமைக்கப்பட்டது", ar: "✓ تم التكوين" },
  "fs.chipNotConfigured": { en: "Not configured. Uses account defaults", ms: "Belum dikonfigurasi — guna lalai akaun", zh: "未配置 — 使用账户默认值", ta: "கட்டமைக்கப்படவில்லை — கணக்கு இயல்புநிலைகளைப் பயன்படுத்துகிறது", ar: "غير مهيأ — يستخدم افتراضيات الحساب" },
  "fs.chipBrand": { en: "Brand ID:", ms: "Brand ID:", zh: "Brand ID：", ta: "Brand ID:", ar: "معرف العلامة:" },
  "fs.chipBrandId": { en: "Brand ID", ms: "Brand ID", zh: "Brand ID", ta: "Brand ID", ar: "معرف العلامة" },
  // The sample text INSIDE the two CHIP feed inputs (card t_b432c9ca) — a
  // placeholder key of its own, like login.emailPh, so a locale can word the
  // sample differently from the label above it.
  "fs.chipBrandIdPh": { en: "Brand ID", ms: "ID jenama", zh: "品牌 ID", ta: "பிராண்ட் ID", ar: "معرف العلامة التجارية" },
  "fs.emailFallbackPh": { en: "Email fallback (one-time forms)", ms: "Emel sandaran (borang sekali guna)", zh: "备用邮箱（一次性表单）", ta: "மாற்று மின்னஞ்சல் (ஒரு முறை படிவங்கள்)", ar: "بريد إلكتروني احتياطي (نماذج لمرة واحدة)" },
  "fs.chipSaved": { en: "CHIP feed saved ✓", ms: "Feed CHIP disimpan ✓", zh: "CHIP 配置已保存 ✓", ta: "CHIP ஃபீட் சேமிக்கப்பட்டது ✓", ar: "تم حفظ خلاصة CHIP ✓" },
  "fs.chipFail": { en: "Failed to save CHIP feed", ms: "Gagal simpan feed CHIP", zh: "保存 CHIP 配置失败", ta: "CHIP ஃபீட்டைச் சேமிக்க முடியவில்லை", ar: "فشل حفظ خلاصة CHIP" },
  "fs.chipCleared": { en: "CHIP feed cleared. Account defaults apply", ms: "Feed CHIP dikosongkan — lalai akaun terpakai", zh: "CHIP 配置已清除 — 应用账户默认值", ta: "CHIP ஃபீட் அழிக்கப்பட்டது — கணக்கு இயல்புநிலைகள் பொருந்தும்", ar: "تم مسح خلاصة CHIP — تنطبق افتراضيات الحساب" },
  // issue #196: per-form webhook overrides
  "fs.webhooks": { en: "Webhooks", ms: "Webhook", zh: "Webhook", ta: "வெப்ஹூக்குகள்", ar: "خطافات الويب" },
  "fs.webhooksHint": { en: "Per-form webhooks fire only for this form's events. Workspace-wide webhooks (in Settings → Webhooks) still fire for all forms.", ms: "Webhook per-borang hanya untuk peristiwa borang ini. Webhook seluruh ruang kerja (dalam Tetapan → Webhook) tetap untuk semua borang.", zh: "每表单 Webhook 仅针对此表单的事件触发。空间级 Webhook（设置 → Webhook）仍会为所有表单触发。", ta: "ஒவ்வொரு படிவ வெப்ஹூக்குகள் இந்த படிவத்தின் நிகழ்வுகளுக்கு மட்டுமே. இடம்-அளவிலான வெப்ஹூக்குகள் (அமைப்புகள் → வெப்ஹூக்குகள்) அனைத்து படிவங்களுக்கும்.", ar: "خطافات الويب لكل نموذج تُطلق فقط لأحداث هذا النموذج. خطافات الويب على مستوى المساحة (الإعدادات ← خطافات الويب) ما زالت تُطلق لجميع النماذج." },
  "fs.webhooksEmpty": { en: "No per-form webhooks yet. Add one to receive this form's events.", ms: "Belum ada webhook per-borang. Tambah satu untuk terima peristiwa borang ini.", zh: "暂无每表单 Webhook。添加一个以接收此表单的事件。", ta: "இன்னும் படிவ வெப்ஹூக்குகள் இல்லை. இந்த படிவத்தின் நிகழ்வுகளைப் பெற ஒன்றைச் சேர்க்கவும்.", ar: "لا توجد خطافات ويب لكل نموذج بعد. أضف واحداً لاستقبال أحداث هذا النموذج." },
  "fs.webhookAdded": { en: "Webhook added ✓", ms: "Webhook ditambah ✓", zh: "Webhook 已添加 ✓", ta: "வெப்ஹூக் சேர்க்கப்பட்டது ✓", ar: "تمت إضافة خطاف الويب ✓" },
  "fs.webhookDeleted": { en: "Webhook deleted", ms: "Webhook dipadam", zh: "Webhook 已删除", ta: "வெப்ஹூக் நீக்கப்பட்டது", ar: "تم حذف خطاف الويب" },
  "fs.webhookFail": { en: "Failed to save webhook", ms: "Gagal simpan webhook", zh: "保存 Webhook 失败", ta: "வெப்ஹூக்கைச் சேமிக்க முடியவில்லை", ar: "فشل حفظ خطاف الويب" },
  // Issue #208: multiple per-form CHIP feeds with conditional logic.
  "fs.chipFeedsTitle": { en: "CHIP Feeds", ms: "Feed CHIP", zh: "CHIP 配置", ta: "CHIP ஃபீட்கள்", ar: "خلاصات CHIP" },
  "fs.chipFeedsHint": { en: "Route a submission to a specific CHIP brand based on its field values. Feeds are evaluated in order; the first matching feed is used. A feed with no condition is the default and always matches.", ms: "Halakan penyerahan ke brand CHIP tertentu berdasarkan nilai medannya. Feed dinilai mengikut urutan; feed pertama yang padan digunakan. Feed tanpa condition ialah lalai dan sentiasa padan.", zh: "根据字段值将提交路由到特定 CHIP 品牌。按顺序评估配置；使用第一个匹配的配置。无条件的配置为默认值，始终匹配。", ta: "புல மதிப்புகளின் அடிப்படையில் சமர்ப்பிப்பை குறிப்பிட்ட CHIP பிராண்டிற்கு வழிநடத்தவும். ஃபீட்கள் வரிசையில் மதிப்பிடப்படுகின்றன; முதல் பொருந்தும் ஃபீட் பயன்படுத்தப்படுகிறது. நிபந்தனை இல்லாத ஃபீட் இயல்புநிலை மற்றும் எப்போதும் பொருந்தும்.", ar: "وجّه الإرسال إلى علامة CHIP محددة بناءً على قيم حقوله. تُقيَّم الخلاصات بالترتيب؛ تُستخدم أول خلاصة مطابقة. الخلاصة بدون شرط هي الافتراضية وتطابق دائماً." },
  "fs.chipFeedName": { en: "Feed name", ms: "Nama feed", zh: "配置名称", ta: "ஃபீட் பெயர்", ar: "اسم الخلاصة" },
  "fs.chipFeedNamePh": { en: "e.g. Brand A feed", ms: "cth. Feed Brand A", zh: "例如：品牌 A 配置", ta: "எ.கா. பிராண்ட் A ஃபீட்", ar: "مثال: خلاصة العلامة أ" },
  "fs.chipFeedDefault": { en: "Default (always matches)", ms: "Lalai (sentiasa padan)", zh: "默认（始终匹配）", ta: "இயல்புநிலை (எப்போதும் பொருந்தும்)", ar: "افتراضي (يطابق دائماً)" },
  "fs.chipFeedConditional": { en: "Conditional logic", ms: "Logik bersyarat", zh: "条件逻辑", ta: "நிபந்தனை தர்க்கம்", ar: "منطق شرطي" },
  "fs.chipFeedIfField": { en: "If field", ms: "Jika medan", zh: "如果字段", ta: "புலம் என்றால்", ar: "إذا كان الحقل" },
  "fs.chipFeedNoCondition": { en: "No condition (default feed)", ms: "Tiada condition (feed lalai)", zh: "无条件（默认配置）", ta: "நிபந்தனை இல்லை (இயல்புநிலை ஃபீட்)", ar: "بدون شرط (خلاصة افتراضية)" },
  "fs.chipFeedAdd": { en: "Add feed", ms: "Tambah feed", zh: "添加配置", ta: "ஃபீட் சேர்க்க", ar: "إضافة خلاصة" },
  "fs.chipFeedRemove": { en: "Remove", ms: "Buang", zh: "删除", ta: "அகற்று", ar: "إزالة" },
  "fs.chipFeedConfigured": { en: "✓ configured", ms: "✓ dikonfigurasi", zh: "✓ 已配置", ta: "✓ கட்டமைக்கப்பட்டது", ar: "✓ مهيأ" },
  "fs.chipFeedNotConfigured": { en: "missing keys", ms: "kunci tiada", zh: "缺少密钥", ta: "விசைகள் இல்லை", ar: "مفاتيح مفقودة" },
  "fs.chipFeedsSaved": { en: "CHIP feeds saved ✓", ms: "Feed CHIP disimpan ✓", zh: "CHIP 配置已保存 ✓", ta: "CHIP ஃபீட்கள் சேமிக்கப்பட்டன ✓", ar: "تم حفظ خلاصات CHIP ✓" },
  "fs.chipFeedsFail": { en: "Failed to save CHIP feeds", ms: "Gagal simpan feed CHIP", zh: "保存 CHIP 配置失败", ta: "CHIP ஃபீட்களைச் சேமிக்க முடியவில்லை", ar: "فشل حفظ خلاصات CHIP" },
  "fs.chipSecretKeep": { en: "(blank = keep existing key)", ms: "(kosong = kekal kunci sedia ada)", zh: "（留空 = 保留现有密钥）", ta: "(காலியாக = தற்போதைய விசையை வைத்திரு)", ar: "(فارغ = الاحتفاظ بالمفتاح الحالي)" },
  // shipping by zone (the managed rate source a form's shipping block can use)
  "fs.ship": { en: "Shipping", ms: "Penghantaran", zh: "运输", ta: "ஷிப்பிங்", ar: "الشحن" },
  "fs.shipHint": { en: "A shipping block set to Managed zones charges the zone the buyer's address resolves to. The fee is recorded as shipping on the sale, never as a line item.", ms: "Zon penghantaran dan kadar yang dikongsi oleh setiap borang dalam ruang kerja ini. Blok penghantaran yang sumber kadarnya Zon terurus mengenakan kadar zon yang dipadankan dengan alamat pembeli, dan bayaran itu direkodkan sebagai penghantaran pada jualan — bukan sebagai item baris.", zh: "此工作区所有表单共享的配送区域与费率。费率来源为「托管区域」的运输字段会按买家地址匹配区域收费，费用记为销售的运费——不会作为明细项。", ta: "இந்த பணியிடத்தின் அனைத்து படிவங்களும் பகிரும் விநியோக மண்டலங்களும் கட்டணங்களும். கட்டண மூலம் «நிர்வகிக்கப்படும் மண்டலங்கள்» ஆக உள்ள ஷிப்பிங் புலம், வாங்குபவரின் முகவரிக்கு பொருந்தும் மண்டலக் கட்டணத்தை வசூலிக்கும்; அது விற்பனையின் ஷிப்பிங் தொகையாகப் பதிவாகும் — வரி உருப்படியாக அல்ல.", ar: "مناطق التوصيل وأسعارها، مشتركة بين كل نماذج هذه المساحة. حقل الشحن الذي مصدره «مناطق مُدارة» يفرض سعر المنطقة التي يطابقها عنوان المشتري، ويُسجَّل الرسم كشحن على عملية البيع — لا كبند." },
  "fs.shipUnused": { en: "No form in this workspace uses managed zones yet. Set a shipping block's rate source to Managed zones to charge these rates.", ms: "Belum ada borang dalam ruang kerja ini menggunakan zon terurus. Tetapkan sumber kadar blok penghantaran kepada Zon terurus untuk mengenakan kadar ini.", zh: "此工作区暂无表单使用托管区域。将运输字段的费率来源设为「托管区域」即可使用这些费率。", ta: "இந்த பணியிடத்தில் எந்தப் படிவும் நிர்வகிக்கப்படும் மண்டலங்களைப் பயன்படுத்தவில்லை. இந்தக் கட்டணங்களை வசூலிக்க ஷிப்பிங் புலத்தின் கட்டண மூலத்தை «நிர்வகிக்கப்படும் மண்டலங்கள்» ஆக அமைக்கவும்.", ar: "لا يوجد نموذج في هذه المساحة يستخدم المناطق المُدارة بعد. اضبط مصدر السعر في حقل الشحن إلى «مناطق مُدارة» لفرض هذه الأسعار." },
  "fs.shipSets": { en: "Zone sets", ms: "Set zon", zh: "区域组", ta: "மண்டல தொகுப்புகள்", ar: "مجموعات المناطق" },
  "fs.shipEmpty": { en: "No zone set yet. Add one, then add the zones and their rates.", ms: "Belum ada set zon. Tambah satu, kemudian tambah zon dan kadarnya.", zh: "暂无区域组。请先添加一个，然后添加区域及其费率。", ta: "இன்னும் மண்டல தொகுப்பு இல்லை. ஒன்றைச் சேர்த்து, பின்னர் மண்டலங்களையும் கட்டணங்களையும் சேர்க்கவும்.", ar: "لا توجد مجموعة مناطق بعد. أضف واحدة، ثم أضف المناطق وأسعارها." },
  "fs.shipAddSet": { en: "Add zone set", ms: "Tambah set zon", zh: "添加区域组", ta: "மண்டல தொகுப்பு சேர்", ar: "إضافة مجموعة مناطق" },
  "fs.shipSetName": { en: "Set name", ms: "Nama set", zh: "组名称", ta: "தொகுப்பு பெயர்", ar: "اسم المجموعة" },
  "fs.shipSetDefault": { en: "Default set", ms: "Set lalai", zh: "默认组", ta: "இயல்புநிலை தொகுப்பு", ar: "المجموعة الافتراضية" },
  "fs.shipAddZone": { en: "Add zone", ms: "Tambah zon", zh: "添加区域", ta: "மண்டலம் சேர்", ar: "إضافة منطقة" },
  "fs.shipZoneName": { en: "Zone name", ms: "Nama zon", zh: "区域名称", ta: "மண்டல பெயர்", ar: "اسم المنطقة" },
  "fs.shipStates": { en: "States (comma separated; peninsular or east_malaysia expand to the whole group)", ms: "Negeri (pisah dengan koma; peninsular atau east_malaysia meliputi seluruh kumpulan)", zh: "州属（逗号分隔；peninsular 或 east_malaysia 代表整组）", ta: "மாநிலங்கள் (கமாவால் பிரிக்கவும்; peninsular அல்லது east_malaysia முழு குழுவையும் குறிக்கும்)", ar: "الولايات (مفصولة بفواصل؛ peninsular أو east_malaysia تشمل المجموعة كاملة)" },
  "fs.shipDistricts": { en: "Districts / cities (comma separated)", ms: "Daerah / bandar (pisah dengan koma)", zh: "地区／城市（逗号分隔）", ta: "மாவட்டங்கள் / நகரங்கள் (கமாவால் பிரிக்கவும்)", ar: "المناطق / المدن (مفصولة بفواصل)" },
  "fs.shipPostcodeFrom": { en: "Postcode from", ms: "Poskod mula", zh: "起始邮编", ta: "அஞ்சல் குறியீடு முதல்", ar: "الرمز البريدي من" },
  "fs.shipPostcodeTo": { en: "Postcode to", ms: "Poskod akhir", zh: "结束邮编", ta: "அஞ்சல் குறியீடு வரை", ar: "الرمز البريدي إلى" },
  "fs.shipPriority": { en: "Priority (lower is tested first)", ms: "Keutamaan (kecil diuji dahulu)", zh: "优先顺序（数值小的先匹配）", ta: "முன்னுரிமை (சிறியது முதலில் சோதிக்கப்படும்)", ar: "الأولوية (الأصغر يُفحص أولاً)" },
  "fs.shipFallback": { en: "Fallback zone (used only when nothing else matches)", ms: "Zon lalai (hanya bila tiada zon lain padan)", zh: "回退区域（仅在其他区域都不匹配时使用）", ta: "இயல்புநிலை மண்டலம் (வேறு எதுவும் பொருந்தாதபோது மட்டும்)", ar: "منطقة احتياطية (تُستخدم فقط عند عدم مطابقة أي منطقة أخرى)" },
  "fs.shipRates": { en: "Rates", ms: "Kadar", zh: "费率", ta: "கட்டணங்கள்", ar: "الأسعار" },
  "fs.shipRateLabel": { en: "Rate name", ms: "Nama kadar", zh: "费率名称", ta: "கட்டண பெயர்", ar: "اسم السعر" },
  "fs.shipRatePrice": { en: "Price ({cur})", ms: "Harga ({cur})", zh: "价格（{cur}）", ta: "விலை ({cur})", ar: "السعر ({cur})" },
  "fs.shipRateMinW": { en: "Min weight (g)", ms: "Berat min (g)", zh: "最小重量（g）", ta: "குறைந்த எடை (g)", ar: "أقل وزن (غ)" },
  "fs.shipRateMaxW": { en: "Max weight (g)", ms: "Berat maks (g)", zh: "最大重量（g）", ta: "அதிக எடை (g)", ar: "أقصى وزن (غ)" },
  "fs.shipFreeOver": { en: "Free over ({cur})", ms: "Percuma melebihi ({cur})", zh: "满额免运费（{cur}）", ta: "இதற்கு மேல் இலவசம் ({cur})", ar: "مجاني بعد ({cur})" },
  "fs.shipAddRate": { en: "Add rate", ms: "Tambah kadar", zh: "添加费率", ta: "கட்டணம் சேர்", ar: "إضافة سعر" },
  "fs.shipSave": { en: "Save shipping rates", ms: "Simpan kadar penghantaran", zh: "保存运费", ta: "ஷிப்பிங் கட்டணங்களைச் சேமி", ar: "حفظ أسعار الشحن" },
  "fs.shipSaved": { en: "Shipping rates saved ✓", ms: "Kadar penghantaran disimpan ✓", zh: "运费已保存 ✓", ta: "ஷிப்பிங் கட்டணங்கள் சேமிக்கப்பட்டன ✓", ar: "تم حفظ أسعار الشحن ✓" },
  "fs.shipSaveFail": { en: "Failed to save shipping rates", ms: "Gagal simpan kadar penghantaran", zh: "保存运费失败", ta: "ஷிப்பிங் கட்டணங்களைச் சேமிக்க முடியவில்லை", ar: "فشل حفظ أسعار الشحن" },
  "fs.shipLoadFail": { en: "Could not load the shipping rates", ms: "Tidak dapat memuatkan kadar penghantaran", zh: "无法加载运费", ta: "ஷிப்பிங் கட்டணங்களை ஏற்ற முடியவில்லை", ar: "تعذّر تحميل أسعار الشحن" },
  "fs.shipDelete": { en: "Delete", ms: "Padam", zh: "删除", ta: "நீக்கு", ar: "حذف" },
  "fs.shipGuide": { en: "How shipping by zone works", ms: "Cara penghantaran mengikut zon berfungsi", zh: "按区域计费如何运作", ta: "மண்டல வாரியாக ஷிப்பிங் எவ்வாறு இயங்குகிறது", ar: "كيف يعمل الشحن حسب المنطقة" },
  // Courier providers (shipping phase 2). The connect step belongs to the ACCOUNT
  // OWNER: for EasyParcel the dashboard hands him a LINK to open on his own
  // device, and nobody on this side ever sees or asks for his password.
  "fs.couriers": { en: "Courier providers", ms: "Pembekal penghantaran", zh: "快递服务商", ta: "கூரியர் வழங்குநர்கள்", ar: "مزودو الشحن" },
  "fs.couriersHint": { en: "Connect a courier to price delivery from a live quote at checkout. If the quote fails, times out or the courier is unavailable, the zone rates above are charged instead, and the sale records which one applied. A quote needs the pickup point below and a point on each zone.", ms: "Sambungkan kurier supaya kos penghantaran disebut secara langsung semasa bayar. Jika sebut harga gagal, tamat masa atau kurier tidak tersedia, kadar zon di atas akan dikenakan, dan jualan merekodkan yang mana satu digunakan. Sebut harga memerlukan titik ambil di bawah dan titik pada setiap zon.", zh: "连接快递后，运费将在结账时按实时报价计算。若报价失败、超时或快递不可用，则改用上方的区域费率，销售记录会注明采用哪一种。报价需要下方的取件点以及每个区域的一个点。", ta: "கூரியரை இணைத்தால் பணம் செலுத்தும்போது நேரடி மதிப்பீட்டின்படி ஷிப்பிங் கட்டணம் கணக்கிடப்படும். மதிப்பீடு தோல்வியடைந்தால், நேரம் முடிந்தால் அல்லது கூரியர் கிடைக்கவில்லை என்றால் மேலே உள்ள மண்டலக் கட்டணம் வசூலிக்கப்படும்; எது பயன்பட்டது என்பது விற்பனையில் பதிவாகும்.", ar: "اربط شركة شحن ليُحتسب الشحن من عرض سعر مباشر عند الدفع. إذا فشل العرض أو انتهت مهلته أو كانت الشركة غير متاحة، تُطبَّق أسعار المناطق أعلاه، ويسجّل البيع أيّهما استُخدم." },
  "fs.courierCredentials": { en: "Server credentials", ms: "Kredensial pelayan", zh: "服务器凭据", ta: "சேவையகச் சான்றுகள்", ar: "بيانات اعتماد الخادم" },
  "fs.courierConfigured": { en: "configured", ms: "dikonfigurasi", zh: "已配置", ta: "அமைக்கப்பட்டது", ar: "مُهيّأ" },
  "fs.courierNotConfigured": { en: "not configured yet", ms: "belum dikonfigurasi", zh: "尚未配置", ta: "இன்னும் அமைக்கப்படவில்லை", ar: "غير مُهيّأ بعد" },
  "fs.courierEnvSandbox": { en: "sandbox (simulated)", ms: "sandbox (simulasi)", zh: "沙盒（模拟）", ta: "சாண்ட்பாக்ஸ் (உருவகிப்பு)", ar: "بيئة تجريبية (محاكاة)" },
  "fs.courierEnvLive": { en: "live", ms: "langsung", zh: "正式", ta: "நேரடி", ar: "حيّ" },
  "fs.courierEnvUnknown": { en: "environment unknown", ms: "persekitaran tidak diketahui", zh: "环境未知", ta: "சூழல் தெரியவில்லை", ar: "البيئة غير معروفة" },
  "fs.courierConnected": { en: "Connected", ms: "Disambung", zh: "已连接", ta: "இணைக்கப்பட்டது", ar: "متصل" },
  "fs.courierNotConnected": { en: "Not connected", ms: "Belum disambung", zh: "未连接", ta: "இணைக்கப்படவில்லை", ar: "غير متصل" },
  "fs.courierConnect": { en: "Connect", ms: "Sambung", zh: "连接", ta: "இணை", ar: "ربط" },
  "fs.courierDisconnect": { en: "Disconnect", ms: "Putuskan", zh: "断开", ta: "இணைப்பை நீக்கு", ar: "فصل" },
  "fs.courierOrigin": { en: "Where you ship from", ms: "Dari mana anda menghantar", zh: "发货地", ta: "நீங்கள் எங்கிருந்து அனுப்புகிறீர்கள்", ar: "من أين تشحن" },
  "fs.courierLat": { en: "Pickup latitude", ms: "Latitud tempat ambil", zh: "取件纬度", ta: "எடுக்கும் இடத்தின் அகலாங்கு", ar: "خط عرض الاستلام" },
  "fs.courierLng": { en: "Pickup longitude", ms: "Longitud tempat ambil", zh: "取件经度", ta: "எடுக்கும் இடத்தின் நெட்டாங்கு", ar: "خط طول الاستلام" },
  "fs.courierPostcode": { en: "Pickup postcode", ms: "Poskod tempat ambil", zh: "取件邮编", ta: "எடுக்கும் இட அஞ்சல் குறியீடு", ar: "الرمز البريدي للاستلام" },
  "fs.courierState": { en: "Pickup state", ms: "Negeri tempat ambil", zh: "取件州属", ta: "எடுக்கும் இட மாநிலம்", ar: "ولاية الاستلام" },
  "fs.courierMissing": { en: "Still needed", ms: "Masih diperlukan", zh: "仍需完成", ta: "இன்னும் தேவை", ar: "لا يزال مطلوبًا" },
  "fs.courierOpenLink": { en: "Open this link to authorize the account. The account owner must open it (any device); nobody should ever ask you for your password.", ms: "Buka pautan ini untuk memberi kebenaran kepada akaun. Pemilik akaun mesti membukanya (mana-mana peranti); jangan sesiapa meminta kata laluan anda.", zh: "打开此链接以授权账户。账户所有者必须自行打开（任何设备）；任何人都不得向您索取密码。", ta: "கணக்கிற்கு அனுமதி அளிக்க இந்த இணைப்பைத் திறக்கவும். கணக்கு உரிமையாளரே அதைத் திறக்க வேண்டும் (எந்தச் சாதனமும்); யாரும் உங்கள் கடவுச்சொல்லைக் கேட்கக் கூடாது.", ar: "افتح هذا الرابط لتفويض الحساب. على مالك الحساب فتحه بنفسه (من أي جهاز)؛ ولا يجوز لأي أحد أن يطلب كلمة المرور." },
  "fs.courierCallback": { en: "Callback URL to register", ms: "URL panggil balik untuk didaftarkan", zh: "需登记的授权回调地址", ta: "பதிவு செய்ய வேண்டிய அழைப்பு URL", ar: "رابط الاستدعاء المطلوب تسجيله" },
  "fs.courierConnectFailed": { en: "Could not connect this courier", ms: "Gagal menyambung pembekal ini", zh: "无法连接该快递", ta: "இந்தக் கூரியரை இணைக்க முடியவில்லை", ar: "تعذّر ربط مزود الشحن" },
  // landing
  // The badge states the two claims that are true of the product without
  // qualification: the price and the licence. It used to carry a third clause
  // naming the payment processor, which made the product's one-line identity
  // claim about one provider rather than about what JomForm costs and how it is
  // licensed. The processor is still named where it is a fact — page metadata
  // ("accept CHIP payments"), land.pricebody ("You pay CHIP's payment fee…") and
  // /docs. licenseSurfaces.test.ts still requires MIT in every locale and
  // "free" in English; both remaining claims satisfy it.
  "land.badge": { en: "100% free · open source (MIT)", ms: "100% percuma · sumber terbuka (MIT)", zh: "100% 免费 · 开源 (MIT)", ta: "100% இலவசம் · திறந்த மூல (MIT)", ar: "مجاني 100% · مفتوح المصدر (MIT)" },
  "land.hero1": { en: "Sell online with", ms: "Jual secara online dengan", zh: "5 分钟建立订单表单", ta: "5 நிமிடங்களில் ஆர்டர் படிவம்", ar: "أنشئ نموذج طلب في 5 دقائق" },
  "land.hero2": { en: "beautiful order forms.", ms: "borang tempahan cantik.", zh: "通过 CHIP 收款", ta: "CHIP மூலம் பணம் வசூலிக்கவும்", ar: "اقبل المدفوعات عبر CHIP" },
  "land.hero3": { en: "Let an AI agent manage your store", ms: "Biarkan agent AI mengurus kedai anda", zh: "让 AI 代理管理您的商店", ta: "AI முகவர் உங்கள் கடையை நிர்வகிக்கட்டும்", ar: "دع الوكيل الذكي يدير متجرك" },
  "land.sub": { en: "Build a form, get paid online, manage every sale. Free forever for Malaysian social sellers.", ms: "Bina borang, buyer bayar secara online, anda urus jualan. Untuk penjual sosial Malaysia — percuma selamanya.", zh: "为马来西亚社交卖家打造的表单、支付与销售管理。", ta: "மலேசிய சமூக விற்பனையாளர்களுக்கான படிவங்கள், பணம், விற்பனை.", ar: "نماذج ومدفوعات ومبيعات للبائعين على وسائل التواصل في ماليزيا." },
  "land.cta1": { en: "Start free →", ms: "Mula percuma →", zh: "免费开始", ta: "இலவசமாக தொடங்கு", ar: "ابدأ مجاناً" },
  "land.cta2": { en: "How it works", ms: "Macam mana ia berfungsi", zh: "查看演示", ta: "டெமோ பார்க்க", ar: "شاهد العرض" },
  "land.buildForm": { en: "Build a form", ms: "Bina borang", zh: "+ 创建表单", ta: "+ படிவம் உருவாக்கு", ar: "+ أنشئ نموذجاً" },
  "land.login": { en: "Log in", ms: "Log masuk", zh: "登录", ta: "உள்நுழை", ar: "تسجيل الدخول" },
  /* The phone header's menu button (card t_2b2da6a7). "Menu" is the same noun in
     Malay, so the two locales this page offers read naturally; the state is
     carried by `aria-expanded`, not by a second name. */
  "land.menu": { en: "Menu", ms: "Menu", zh: "菜单", ta: "மெனு", ar: "القائمة" },
  /* ── landing redesign (card t_09f60795) ───────────────────────────────────
     The product-led hero, its three-step lane, the showcase section head, the
     MCP terminal transcript and the "Free. Not a trial." block. Every key is
     in all five locales; `land.fmpay` carries {amount} in all five. */
  "land.trust1": { en: "No monthly fee", ms: "Tiada yuran bulanan", zh: "无月费", ta: "மாதக் கட்டணம் இல்லை", ar: "بلا رسوم شهرية" },
  "land.trust2": { en: "No credit card to start", ms: "Tiada kad kredit untuk mula", zh: "开始无需信用卡", ta: "தொடங்க அட்டை தேவையில்லை", ar: "لا حاجة لبطاقة للبدء" },
  "land.trust3": { en: "Open source (MIT)", ms: "Sumber terbuka (MIT)", zh: "开源 (MIT)", ta: "திறந்த மூல (MIT)", ar: "مفتوح المصدر (MIT)" },
  "land.fmname": { en: "Name", ms: "Nama", zh: "姓名", ta: "பெயர்", ar: "الاسم" },
  "land.fmwa": { en: "WhatsApp", ms: "WhatsApp", zh: "WhatsApp", ta: "WhatsApp", ar: "واتساب" },
  "land.fmsubtotal": { en: "Subtotal", ms: "Jumlah kecil", zh: "小计", ta: "துணைத் தொகை", ar: "المجموع الفرعي" },
  "land.fmshipping": { en: "Shipping: J&T, Klang Valley", ms: "Penghantaran — J&T, Lembah Klang", zh: "运费 — J&T，巴生谷", ta: "கட்டணம் — J&T, கிள்ளான் பள்ளத்தாக்கு", ar: "الشحن — J&T، وادي كلانج" },
  "land.fmtotal": { en: "Total", ms: "Jumlah", zh: "合计", ta: "மொத்தம்", ar: "الإجمالي" },
  "land.fmpay": { en: "Pay {amount}", ms: "Bayar {amount}", zh: "支付 {amount}", ta: "{amount} செலுத்து", ar: "ادفع {amount}" },
  /* card t_453489b9 — the demo's payment-method picker, mirroring the product's
     own picker (api/internal/httpapi/paymethod.go renders the same rows). The
     labels are the GROUPS, in the product's order: FPX, FPX B2B1, DuitNow QR, card
     and e-wallet. FPX, FPX B2B1 and DuitNow QR are scheme names and read the same
     in every locale; card and e-wallet are ordinary nouns. Card t_1d0198e3 split
     FPX into its two products (B2C personal / B2B1 corporate) because each carries
     its own fee — the demo shows both, exactly as the real picker does.
     `land.payvia` is the Pay button's second line. */
  "land.paytitle": { en: "Payment method", ms: "Kaedah bayaran", zh: "付款方式", ta: "கட்டண முறை", ar: "طريقة الدفع" },
  "land.paypick": { en: "Choose one", ms: "Pilih satu", zh: "选择一项", ta: "ஒன்றைத் தேர்ந்தெடு", ar: "اختر واحدة" },
  "land.payfpx": { en: "FPX", ms: "FPX", zh: "FPX", ta: "FPX", ar: "FPX" },
  "land.payfpxdesc": { en: "Personal online banking: Maybank2u, CIMB Clicks, RHB Now", ms: "Perbankan online peribadi: Maybank2u, CIMB Clicks, RHB Now", zh: "个人网上银行：Maybank2u、CIMB Clicks、RHB Now", ta: "தனிநபர் ஆன்லைன் வங்கி: Maybank2u, CIMB Clicks, RHB Now", ar: "الخدمات المصرفية الشخصية عبر الإنترنت: Maybank2u وCIMB Clicks وRHB Now" },
  "land.payfpxb2b1": { en: "FPX B2B1", ms: "FPX B2B1", zh: "FPX B2B1", ta: "FPX B2B1", ar: "FPX B2B1" },
  "land.payfpxb2b1desc": { en: "Corporate online banking: for business accounts", ms: "Perbankan online korporat: untuk akaun perniagaan", zh: "企业网上银行：供公司账户使用", ta: "கார்ப்பரேட் ஆன்லைன் வங்கி: வணிகக் கணக்குகளுக்கு", ar: "الخدمات المصرفية للشركات: لحسابات الأعمال" },
  "land.paydnqr": { en: "DuitNow QR", ms: "DuitNow QR", zh: "DuitNow QR", ta: "DuitNow QR", ar: "DuitNow QR" },
  "land.paydnqrdesc": { en: "Scan with any bank app", ms: "Imbas dengan mana-mana aplikasi bank", zh: "用任何银行应用扫描", ta: "எந்த வங்கி செயலியிலும் ஸ்கேன் செய்யுங்கள்", ar: "امسح بأي تطبيق مصرفي" },
  "land.paycard": { en: "Debit / credit card", ms: "Kad debit / kredit", zh: "借记卡 / 信用卡", ta: "பற்று / கடன் அட்டை", ar: "بطاقة خصم / ائتمان" },
  "land.paycarddesc": { en: "Visa · Mastercard", ms: "Visa · Mastercard", zh: "Visa · Mastercard", ta: "Visa · Mastercard", ar: "Visa · Mastercard" },
  "land.paywallet": { en: "e-Wallet", ms: "E-dompet", zh: "电子钱包", ta: "மின் பணப்பை", ar: "محفظة إلكترونية" },
  "land.paywalletdesc": { en: "GrabPay · ShopeePay · Touch 'n Go", ms: "GrabPay · ShopeePay · Touch 'n Go", zh: "GrabPay · ShopeePay · Touch 'n Go", ta: "GrabPay · ShopeePay · Touch 'n Go", ar: "GrabPay · ShopeePay · Touch 'n Go" },
  "land.payvia": { en: "via {method}", ms: "melalui {method}", zh: "通过 {method}", ta: "{method} வழியாக", ar: "عبر {method}" },
  "land.paynote": { en: "Paid through CHIP. JomForm never stores card numbers", ms: "Dibayar melalui CHIP. JomForm tidak menyimpan nombor kad", zh: "通过 CHIP 付款。JomForm 不会保存卡号", ta: "CHIP மூலம் பணம். JomForm அட்டை எண்ணைச் சேமிக்காது", ar: "الدفع عبر CHIP. لا يحفظ JomForm أرقام البطاقات" },
  "land.path1t": { en: "Build the form", ms: "Bina borang", zh: "创建表单", ta: "படிவத்தை உருவாக்கு", ar: "أنشئ النموذج" },
  "land.path1b": { en: "Drag in products, add a name and WhatsApp field, publish with one click.", ms: "Tarik produk, tambah medan nama dan WhatsApp, terbitkan dengan satu klik.", zh: "拖入商品，加上姓名和 WhatsApp 字段，一键发布。", ta: "தயாரிப்புகளை இழுத்துச் சேர், பெயர் மற்றும் WhatsApp புலத்தைச் சேர், ஒரே கிளிக்கில் வெளியிடு.", ar: "أضف المنتجات وحقلي الاسم وواتساب، ثم انشر بنقرة واحدة." },
  "land.path2t": { en: "Buyer pays", ms: "Pembeli bayar", zh: "买家付款", ta: "வாங்குபவர் செலுத்துகிறார்", ar: "يدفع المشتري" },
  "land.path2b": { en: "Secure checkout by CHIP: card, FPX, DuitNow QR and e-wallets.", ms: "Checkout selamat melalui CHIP — kad, FPX, DuitNow QR dan e-dompet.", zh: "通过 CHIP 安全结账 —— 卡、FPX、DuitNow QR 和电子钱包。", ta: "CHIP வழியாகப் பாதுகாப்பான checkout — கார்டு, FPX, DuitNow QR, மின் பணப்பை.", ar: "دفع آمن عبر CHIP — بطاقة وFPX وDuitNow QR ومحافظ إلكترونية." },
  "land.path3t": { en: "It keeps itself", ms: "Ia mengurus sendiri", zh: "自动维护", ta: "தானாகவே நடக்கும்", ar: "يدير نفسه" },
  "land.path3b": { en: "Sales update themselves and every receipt goes out on its own.", ms: "Status jualan naik sendiri dan setiap resit dihantar automatik.", zh: "销售状态自动更新，每张收据自动发出。", ta: "விற்பனை நிலை தானாகப் புதுப்பிக்கும், ஒவ்வொரு ரசீதும் தானாக அனுப்பப்படும்.", ar: "تتحدث المبيعات تلقائياً ويُرسل كل إيصال من تلقاء نفسه." },
  "land.showcases": { en: "Built for the things people actually sell", ms: "Dibina untuk perkara yang orang benar-benar jual", zh: "为人们真正在卖的东西而造", ta: "மக்கள் உண்மையில் விற்பதற்காக உருவாக்கப்பட்டது", ar: "مبني لما يبيعه الناس فعلاً" },
  "land.showcasessub": { en: "Seven setups, each with a screenshot guide that walks the exact clicks.", ms: "Tujuh persediaan, setiap satu dengan panduan bergambar yang menunjukkan langkah sebenar.", zh: "七种场景，每一种都配有截图指南，写明具体步骤。", ta: "ஏழு அமைப்புகள், ஒவ்வொன்றுக்கும் சரியான படிகளைக் காட்டும் பட வழிகாட்டி.", ar: "سبعة إعدادات، ولكل منها دليل بالصور يوضح الخطوات الفعلية." },
  "land.term1": { en: "you › how many sales today?", ms: "anda › berapa jualan hari ini?", zh: "你 › 今天有多少笔销售？", ta: "நீங்கள் › இன்று எத்தனை விற்பனை?", ar: "أنت › كم عدد المبيعات اليوم؟" },
  "land.term2": { en: "agent › 14 paid · RM2,410.00 · 2 pending", ms: "agen › 14 dibayar · RM2,410.00 · 2 menunggu", zh: "代理 › 14 笔已付款 · RM2,410.00 · 2 笔待处理", ta: "முகவர் › 14 செலுத்தப்பட்டது · RM2,410.00 · 2 நிலுவையில்", ar: "الوكيل › 14 مدفوعة · RM2,410.00 · 2 معلّقة" },
  "land.term3": { en: "you › refund sale JF-3f9a2c", ms: "anda › pulangkan jualan JF-3f9a2c", zh: "你 › 退款销售 JF-3f9a2c", ta: "நீங்கள் › JF-3f9a2c விற்பனையைத் திருப்பிச் செலுத்து", ar: "أنت › استرد البيع JF-3f9a2c" },
  "land.term4": { en: "agent › refunded RM186.00 · back to the card", ms: "agen › RM186.00 dipulangkan · kembali ke kad", zh: "代理 › 已退款 RM186.00 · 退回原卡", ta: "முகவர் › RM186.00 திருப்பியளிக்கப்பட்டது · கார்டுக்கே திரும்பியது", ar: "الوكيل › تم استرداد RM186.00 · عادت إلى البطاقة" },
  "land.pricetitle": { en: "Free. Not a trial.", ms: "Percuma. Bukan percubaan.", zh: "免费，不是试用。", ta: "இலவசம். சோதனை அல்ல.", ar: "مجاناً. ليست فترة تجريبية." },
  "land.pricebody": { en: "There is no subscription and no per-form charge. You pay CHIP's payment fee on the transactions you actually make.", ms: "Tiada langganan dan tiada caj setiap borang — anda hanya bayar yuran pembayaran CHIP atas transaksi yang anda benar-benar buat.", zh: "没有订阅费，也没有按表单收费 —— 只按你实际完成的交易收取 CHIP 支付手续费。", ta: "சந்தா இல்லை, படிவுக்குக் கட்டணம் இல்லை — நீங்கள் உண்மையில் செய்யும் பரிவர்த்தனைகளுக்கு CHIP-இன் கட்டணம் மட்டுமே.", ar: "لا اشتراك ولا رسم لكل نموذج — تدفع رسوم الدفع من CHIP على المعاملات التي تجريها فعلاً." },
  "land.f1": { en: "An order form in 5 minutes. Drag in products, fields and images. No code.", ms: "Borang siap 5 minit — tarik produk, medan, imej. Tanpa kod.", zh: "拖放式表单构建器", ta: "இழுத்து விடும் படிவவாக்கி", ar: "منشئ نماذج بالسحب والإفلات" },
  "land.f2": { en: "Buyers pay online the moment they submit. Card, FPX, DuitNow QR, e-wallets.", ms: "Buyer bayar terus selepas submit. Kad, FPX, DuitNow QR, e-wallet.", zh: "CHIP 支付：卡 / FPX / DuitNow QR / 电子钱包", ta: "CHIP பணம்: கார்டு / FPX / DuitNow QR / மின்னணு பணப்பை", ar: "مدفوعات CHIP: بطاقة / FPX / DuitNow QR / محافظ" },
  "land.f3": { en: "AI-agent native: your ChatGPT or Claude agent runs the store over MCP, covering products, prices, sales and refunds.", ms: "AI-agent native: agent ChatGPT/Claude kau kawal seluruh kedai melalui MCP — produk, harga, jualan, refund. API borang paling power di Malaysia.", zh: "MCP 原生：AI 代理连接您的商店", ta: "MCP செயல்படும்: AI முகவர் கடையை இணைக்கும்", ar: "جاهز لـ MCP: وكلاء الذكاء يتصلون بمتجرك" },
  "land.f4": { en: "Sales analytics, CSV export and PDF receipts. Free forever, no monthly fee.", ms: "Analitik jualan, eksport CSV, resit PDF — percuma selamanya, tiada yuran bulanan.", zh: "分析与 CSV 导出", ta: "பகுப்பாய்வு + CSV ஏற்றுமதி", ar: "تحليلات وتصدير CSV" },
  "land.mcptitle": { en: "Control your store with plain language", ms: "Kawal kedai guna bahasa biasa", zh: "您的代理，您的商店", ta: "உங்கள் முகவர், உங்கள் கடை", ar: "وكيلك، متجرك" },
  "land.mcpbody": { en: 'JomForm speaks MCP (Model Context Protocol). Connect your AI agent once, then: \"add product RM25.00\", \"how many sales today?\", \"refund sale JF-xxxx\". All without opening a dashboard.', ms: 'JomForm cakap MCP (Model Context Protocol). Sambung agent AI sekali, lepas tu: \"tambah produk RM20\", \"berapa sales hari ni?\", \"refund jualan JF-xxxx\" — semua tanpa buka dashboard.', zh: 'JomForm 支持 MCP。连接一次 AI 代理：添加商品、查销售、退款——无需仪表板。', ta: 'JomForm MCP பேசுகிறது — டாஷ்போர்டு இல்லாமல் கடை நிர்வகி.', ar: 'JomForm يتحدث MCP — أدر متجرك دون لوحة تحكم.' },
  "land.mcpsnippet": { en: "Connect at https://mcp.jomform.com/mcp → OAuth 2.1 (one-click) → your agent, your store.", ms: "Sambung di https://mcp.jomform.com/mcp → OAuth 2.1 (satu klik) → agent kau, kedai kau.", zh: "在 https://mcp.jomform.com/mcp 连接 → OAuth 2.1（一键）→ 您的代理，您的商店。", ta: "https://mcp.jomform.com/mcp இல் இணைக்கவும் → OAuth 2.1 (ஒரே கிளிக்) → உங்கள் முகவர், உங்கள் கடை.", ar: "اتصل عبر https://mcp.jomform.com/mcp → OAuth 2.1 (بنقرة واحدة) → وكيلك، متجرك." },
  "land.footer": { en: "JomForm. Open source (MIT). Made in Malaysia.", ms: "JomForm — sumber terbuka (MIT). Buatan Malaysia.", zh: "JomForm — 开源 (MIT)。马来西亚制造", ta: "JomForm — திறந்த மூல (MIT). மலேசியாவில் தயாரிக்கப்பட்டது", ar: "JomForm — مفتوح المصدر (MIT). صُنع في ماليزيا" },
  // Operator identity (WANZUL HOSTING ENTERPRISE) — the ONE sentence naming who
  // runs JomForm. Rendered by components/OperatorNotice.tsx on the landing/home
  // footer AND both legal pages, so all three read from this entry; the
  // identifiers it must contain verbatim live in src/lib/operator.ts.
  //
  // The registration numbers (201403298654, PC0012291-H) and the legal name
  // (WANZUL HOSTING ENTERPRISE) are legal identifiers: they stay byte-identical
  // in every locale, and only the sentence around them is translated. Do not
  // translate, reorder or reformat them.
  //
  // RTL: the Arabic string is laid out under dir="rtl" — the passage declares it
  // itself (operatorDirProps) because the legal pages keep an LTR document base
  // — and the clause from the number onwards is wrapped in <bdi dir="ltr"> by
  // OperatorNotice. The parenthesis pair and the sentence-final period would
  // otherwise render inverted — see the measured note in src/lib/operator.ts
  // before editing this.
  "land.operator": {
    en: "JomForm is operated by WANZUL HOSTING ENTERPRISE (Registration No. 201403298654, formerly PC0012291-H).",
    ms: "JomForm dikendalikan oleh WANZUL HOSTING ENTERPRISE — No. Pendaftaran 201403298654 (dahulunya PC0012291-H).",
    zh: "JomForm 由 WANZUL HOSTING ENTERPRISE 运营 — 注册号 201403298654（原 PC0012291-H）。",
    ta: "JomForm WANZUL HOSTING ENTERPRISE ஆல் இயக்கப்படுகிறது — பதிவு எண் 201403298654 (முன்பு PC0012291-H).",
    ar: "يُدار JomForm بواسطة WANZUL HOSTING ENTERPRISE — رقم التسجيل 201403298654 (سابقًا PC0012291-H).",
  },
  // issue #120: MCP setup instructions page (/mcp) + homepage guide link
  "land.mcpguide": { en: "View setup guide", ms: "Lihat cara sambung", zh: "查看设置指南", ta: "அமைப்பு வழிகாட்டியைக் காண்க", ar: "عرض دليل الإعداد" },
  // issue #381: wedding / RSVP use-case showcase
  "land.usecasetitle": { en: "Built for real moments", ms: "Dibina untuk detik sebenar", zh: "为真实时刻而打造", ta: "உண்மையான தருணங்களுக்காக உருவாக்கப்பட்டது", ar: "صُنع للحظات حقيقية" },
  "land.weddingtitle": { en: "Wedding page + RSVP form", ms: "Halaman perkahwinan + borang RSVP", zh: "婚礼页面 + RSVP 表单", ta: "திருமணப் பக்கம் + RSVP படிவம்", ar: "صفحة زفاف + نموذج RSVP" },
  "land.weddingbody": { en: "Publish a wedding page with photos, a theme and an embedded RSVP form.", ms: "Terbitkan halaman perkahwinan dengan foto, tema dan borang RSVP terbina.", zh: "发布带照片、主题和嵌入 RSVP 表单的婚礼页面。", ta: "புகைப்படங்கள், தீம் மற்றும் உட்பொதிக்கப்பட்ட RSVP படிவத்துடன் திருமணப் பக்கத்தை வெளியிடுங்கள்.", ar: "انشر صفحة زفاف بالصور والثيم ونموذج RSVP المدمج." },
  "land.wedding1": { en: "Hero, photos, theme", ms: "Hero, foto, tema", zh: "主视觉、照片、主题", ta: "ஹீரோ, படங்கள், தீம்", ar: "بطل وصور وثيم" },
  "land.wedding2": { en: "Embedded RSVP form", ms: "Borang RSVP terbina", zh: "嵌入 RSVP 表单", ta: "உட்பொதிக்கப்பட்ட RSVP படிவம்", ar: "نموذج RSVP مدمج" },
  "land.wedding3": { en: "Own subdomain or domain", ms: "Subdomain atau domain sendiri", zh: "自有子域名或域名", ta: "சொந்த துணை டொமைன்", ar: "نطاق فرعي أو خاص" },
  "land.wedding4": { en: "Builder or AI agent (MCP)", ms: "Builder atau agent AI (MCP)", zh: "构建器或 AI 代理 (MCP)", ta: "பில்டர் அல்லது AI முகவர் (MCP)", ar: "المحرر أو وكيل الذكاء (MCP)" },
  "land.weddingcta": { en: "Start building →", ms: "Mula bina →", zh: "开始构建 →", ta: "உருவாக்கத் தொடங்கு →", ar: "ابدأ الإنشاء ←" },
  // issue #441: QR ordering (restaurant) use-case showcase
  "land.qrtitle": { en: "QR ordering for restaurants", ms: "Pesanan QR untuk restoran", zh: "餐厅二维码点餐", ta: "உணவகங்களுக்கான QR ஆர்டர்", ar: "الطلب عبر QR للمطاعم" },
  "land.qrbody": { en: "Guests scan the table QR, order and pay online; the order reaches you.", ms: "Tetamu imbas QR di meja, pesan dan bayar dalam talian; pesanan terus sampai.", zh: "顾客扫描桌上二维码，下单并在线支付，订单直达您。", ta: "விருந்தினர் மேசை QR-ஐ ஸ்கேன் செய்து, ஆர்டர் செய்து, ஆன்லைனில் செலுத்துவார்; ஆர்டர் உங்களை அடையும்.", ar: "يمسح الضيوف رمز QR على الطاولة، يطلبون ويدفعون أونلاين؛ يصل الطلب إليك." },
  "land.qr1": { en: "QR code per table", ms: "Kod QR setiap meja", zh: "每桌一个二维码", ta: "ஒவ்வொரு மேசைக்கும் QR", ar: "رمز QR لكل طاولة" },
  "land.qr2": { en: "Digital menu, food photos", ms: "Menu digital, foto makanan", zh: "数字菜单，食物照片", ta: "உணவுப் படங்களுடன் மெனு", ar: "قائمة رقمية وصور طعام" },
  "land.qr3": { en: "Online payment (CHIP)", ms: "Pembayaran dalam talian (CHIP)", zh: "在线支付（CHIP）", ta: "ஆன்லைன் கட்டணம் (CHIP)", ar: "الدفع عبر الإنترنت (CHIP)" },
  "land.qr4": { en: "Order alerts to merchant", ms: "Notifikasi pesanan ke peniaga", zh: "订单通知直达商家", ta: "ஆர்டர் அறிவிப்பு வணிகருக்கு", ar: "إشعار الطلب للتاجر" },
  "land.qrcta": { en: "Set up QR ordering →", ms: "Sediakan pesanan QR →", zh: "设置二维码点餐 →", ta: "QR ஆர்டரை அமைக்கவும் →", ar: "إعداد الطلب عبر QR ←" },
  // card t_345b0c87: recurring class / tuition fees use-case showcase. Every
  // bullet maps to behaviour that exists in the code, and the mapping lives in
  // the section comment in app/landing/page.tsx (the `tpl-tuition` template
  // blocks, the one_time registration fee + its buyer receipt, type=subscription
  // + billing_period month charged on the saved card token by the due-charge
  // sweep, and the /subscriptions page + the subscription MCP tools). No receipt
  // is claimed for the monthly charge: sendReceiptAsync skips a subscription sale.
  "land.tuitiontitle": { en: "Tuition, classes and monthly fees", ms: "Tuisyen, kelas dan yuran bulanan", zh: "补习班与月度费用", ta: "பயிற்சி, வகுப்பு, மாதக் கட்டணம்", ar: "الدروس والصفوف والرسوم الشهرية" },
  "land.tuitionbody": { en: "Take the registration fee now, and charge the monthly class fee to the same card automatically.", ms: "Ambil yuran pendaftaran sekarang, dan caj yuran kelas bulanan ke kad yang sama secara automatik.", zh: "现在收报名费，月费之后自动从同一张卡扣款。", ta: "பதிவுக் கட்டணத்தை இப்போது வசூலிக்கவும்; மாத வகுப்புக் கட்டணம் அதே கார்டிலிருந்து தானாக வசூலாகும்.", ar: "احصل على رسم التسجيل الآن، واخصم الرسم الشهري تلقائياً من البطاقة نفسها." },
  "land.tuition1": { en: "Student, subject, session", ms: "Pelajar, subjek, sesi", zh: "学生、科目、时段", ta: "மாணவர், பாடம், அமர்வு", ar: "الطالب والمادة والحصة" },
  "land.tuition2": { en: "Receipt emailed", ms: "Resit diemel", zh: "收据电邮", ta: "ரசீது மின்னஞ்சல்", ar: "إيصال بالبريد" },
  "land.tuition3": { en: "Monthly fee auto-charged", ms: "Yuran bulanan auto-caj", zh: "月费自动扣", ta: "மாதக் கட்டணம் தானியங்கி", ar: "خصم شهري تلقائي" },
  "land.tuition4": { en: "Cancel from dashboard", ms: "Batal dari dashboard", zh: "仪表板取消", ta: "டாஷ்போர்டில் ரத்து", ar: "إلغاء من اللوحة" },
  "land.tuitioncta": { en: "Build a class form →", ms: "Bina borang kelas →", zh: "创建课程表单 →", ta: "வகுப்புப் படிவம் உருவாக்கு →", ar: "أنشئ نموذج صف ←" },
  // issue #441: Canva→Claude→MCP customization use-case showcase
  "land.canvatitle": { en: "Design with your AI agent", ms: "Reka dengan agent AI anda", zh: "与 AI 代理一起设计", ta: "உங்கள் AI முகவருடன் வடிவமை", ar: "صمّم مع وكيل الذكاء" },
  "land.canvabody": { en: "Design in Canva; your AI agent applies the theme and customises the page.", ms: "Reka dalam Canva; agent AI anda terapkan tema dan sesuaikan halaman.", zh: "在 Canva 设计，AI 代理套用主题并自定义页面。", ta: "Canva-இல் வடிவமைக்கவும்; உங்கள் AI முகவர் தீமைப் பயன்படுத்தி பக்கத்தை மாற்றும்.", ar: "صمّم في Canva؛ ثم دع وكيلك يطبّق الثيم ويخصّص صفحتك." },
  "land.canva1": { en: "Upload media", ms: "Muat naik media", zh: "上传媒体", ta: "மீடியா பதிவேற்று", ar: "رفع الوسائط" },
  "land.canva2": { en: "Set your theme", ms: "Tetapkan tema", zh: "设置主题", ta: "தீம் அமைக்கவும்", ar: "اضبط الثيم" },
  "land.canva3": { en: "Customise with CSS/JS", ms: "Sesuaikan dengan CSS/JS", zh: "用 CSS/JS 自定义", ta: "CSS/JS மாற்றம்", ar: "خصّص بـ CSS/JS" },
  "land.canva4": { en: "MCP-native", ms: "Asli MCP", zh: "MCP 原生", ta: "MCP-சொந்த", ar: "أصلي عبر MCP" },
  "land.canvacta": { en: "Connect your agent →", ms: "Sambung agent anda →", zh: "连接您的代理 →", ta: "உங்கள் முகவரை இணைக்கவும் →", ar: "اربط وكيلك ←" },
  // issue #450: affiliate marketing use-case showcase + feature bullet
  "land.afftitle": { en: "Affiliate marketing built in", ms: "Pemasaran affiliate terbina", zh: "内置联盟营销", ta: "உள்ளமைந்த இணைப்பு சந்தைப்படுத்தல்", ar: "التسويق بالعمولة مدمج" },
  "land.affbody": { en: "Each promoter gets a link; paid sales are attributed and commission calculated automatically.", ms: "Setiap promoter dapat pautan; jualan berbayar dikreditkan dan komisen dikira automatik.", zh: "每位推广者有专属链接；已付款销售自动归属并计算佣金。", ta: "ஒவ்வொரு விளம்பரகர்த்தருக்கும் இணைப்பு; செலுத்தப்பட்ட விற்பனை வரவு, கமிஷன் தானாகக் கணக்கிடப்படும்.", ar: "لكل مسوّق رابطه؛ تُنسب المبيعات المدفوعة وتُحسب العمولة تلقائياً." },
  "land.aff1": { en: "Referral links (?aff=CODE)", ms: "Pautan rujukan (?aff=CODE)", zh: "推荐链接 (?aff=CODE)", ta: "பரிந்துரை இணைப்பு (?aff=CODE)", ar: "روابط الإحالة (?aff=CODE)" },
  "land.aff2": { en: "Auto-attributed paid sales", ms: "Jualan berbayar auto-kredit", zh: "已付款销售自动归属", ta: "தானியங்கி விற்பனை வரவு", ar: "نسبة تلقائية للمبيعات" },
  "land.aff3": { en: "Commission per affiliate", ms: "Komisen setiap affiliate", zh: "按联盟计佣金", ta: "ஒவ்வொரு இணைப்பாளர் கமிஷன்", ar: "عمولة لكل شريك" },
  "land.aff4": { en: "AI agent via MCP", ms: "Agent AI melalui MCP", zh: "AI 代理经 MCP", ta: "AI முகவர் MCP வழியாக", ar: "وكيل AI عبر MCP" },
  "land.affcta": { en: "Set up affiliates →", ms: "Sediakan affiliate →", zh: "设置联盟 →", ta: "இணைப்பாளர்களை அமைக்கவும் →", ar: "إعداد الشركاء ←" },
  // card t_72d8fbac: club / society membership with recurring fees use-case
  // showcase. Every bullet maps to behaviour that exists in the code:
  // billing_period month|year + the hourly due-charge sweep
  // (api/internal/payments/recurring.go), the card token saved at checkout,
  // the /subscriptions dashboard and the list_subscriptions /
  // cancel_subscription / retry_subscription / update_subscription_card MCP
  // tools. A receipt bullet was deliberately NOT written: no code path sends a
  // buyer receipt for a subscription payment (api/internal/payments/purchases.go
  // skips sendReceiptAsync for SaleTypeSubscription, and a recurring charge is
  // not a sale row at all).
  "land.membershiptitle": { en: "Club memberships with recurring fees", ms: "Keahlian kelab, yuran berulang", zh: "俱乐部会员与定期收费", ta: "கிளப் உறுப்பினர், தொடர் கட்டணம்", ar: "عضوية النادي برسوم متكررة" },
  "land.membershipbody": { en: "Members pay once; their card is charged every month or year, hands-free.", ms: "Ahli bayar sekali; kad mereka dicaj setiap bulan atau tahun, tanpa perlu buat apa-apa.", zh: "会员付一次；之后按月或按年从卡自动扣款。", ta: "உறுப்பினர் ஒருமுறை செலுத்துவார்; கார்டு ஒவ்வொரு மாதம் அல்லது ஆண்டு தானாக வசூலாகும்.", ar: "يدفع العضو مرة؛ وتُخصم بطاقته كل شهر أو سنة تلقائياً." },
  "land.membership1": { en: "Auto-charged each period", ms: "Auto-caj setiap tempoh", zh: "每期自动扣款", ta: "ஒவ்வொரு காலமும் தானியங்கி", ar: "خصم تلقائي كل دورة" },
  "land.membership2": { en: "Card saved securely", ms: "Kad disimpan selamat", zh: "卡片安全保存", ta: "கார்டு பாதுகாப்பாக சேமிப்பு", ar: "البطاقة محفوظة بأمان" },
  "land.membership3": { en: "Cancel anytime, card kept", ms: "Batal bila-bila, kad kekal", zh: "随时取消，卡保留", ta: "எப்போதும் ரத்து, கார்டு காக்கும்", ar: "إلغاء بأي وقت، البطاقة تبقى" },
  "land.membership4": { en: "Managed from dashboard", ms: "Urus dari dashboard", zh: "仪表板管理", ta: "டாஷ்போர்டில் நிர்வாகம்", ar: "إدارة من اللوحة" },
  "land.membershipcta": { en: "Set up recurring fees →", ms: "Sediakan yuran berulang →", zh: "设置定期收费 →", ta: "தொடர் கட்டணங்களை அமைக்கவும் →", ar: "إعداد الرسوم المتكررة ←" },
  // Card t_41666fb1 (Wan, 7 Nov) added one "How to set it up" list per showcase here.
  // Card t_b4e5881d (Wan, on a phone screenshot of the RSVP card — "benda yang terlalu
  // technical tak sesuai ada kat landing page") REMOVED those lists from the landing page,
  // so `land.setup` and every `land.<usecase>step1..N` entry is gone from this dictionary:
  // the steps were never the landing page's job, and docs/guides/<slug>/README.md — rendered
  // at /docs/guides/<slug> — carries the real step-by-step with a screenshot per step.
  // src/lib/landingUseCases.test.ts now fails if a setup list comes back to the landing, and
  // it also fails if a linked guide STOPS having numbered steps, so deleting the copy here
  // cannot quietly delete the instructions from the product.
  // Card t_7b9f6d18: /docs/guides/<slug> renders every guide; card t_d6a44ec9 linked
  // the six remaining showcase cards at theirs too (wedding-rsvp, qr-table-ordering,
  // tuition-class, design-with-mcp, affiliate-programme, club-membership).
  // Card t_516bab9d: the "Read the full … guide with screenshots →" sentence became the one
  // word "Guide", because a card was carrying two links doing one job. The visible label is
  // therefore the same on all seven cards and the ACCESSIBLE NAME is what tells them apart:
  // components/landing/ShowcaseCard.tsx renders `aria-label="Guide — <card title>"`, which
  // starts with the visible text (WCAG 2.5.3) and keeps the link list readable.
  // land.guidesoon is kept in the dictionary for any future showcase without a guide — the
  // landing page no longer renders it.
  "land.guidesoon": { en: "Full step-by-step guide with screenshots. Coming soon.", ms: "Panduan penuh langkah demi langkah dengan gambar — akan datang.", zh: "完整图文分步指南 —— 即将推出。", ta: "படங்களுடன் கூடிய முழு படிப்படியான வழிகாட்டி — விரைவில்.", ar: "دليل كامل خطوة بخطوة بالصور — قريباً." },
  "land.guidedonation": { en: "Guide", ms: "Panduan", zh: "指南", ta: "வழிகாட்டி", ar: "الدليل" },
  "land.guidewedding": { en: "Guide", ms: "Panduan", zh: "指南", ta: "வழிகாட்டி", ar: "الدليل" },
  "land.guideqr": { en: "Guide", ms: "Panduan", zh: "指南", ta: "வழிகாட்டி", ar: "الدليل" },
  "land.guidetuition": { en: "Guide", ms: "Panduan", zh: "指南", ta: "வழிகாட்டி", ar: "الدليل" },
  "land.guidecanva": { en: "Guide", ms: "Panduan", zh: "指南", ta: "வழிகாட்டி", ar: "الدليل" },
  "land.guideaff": { en: "Guide", ms: "Panduan", zh: "指南", ta: "வழிகாட்டி", ar: "الدليل" },
  "land.guidemembership": { en: "Guide", ms: "Panduan", zh: "指南", ta: "வழிகாட்டி", ar: "الدليل" },
  "land.donationtitle": { en: "Collect donations with a paid form", ms: "Kutip derma dengan borang berbayar", zh: "用付费表单收集捐款", ta: "கட்டணப் படிவத்தில் நன்கொடை", ar: "اجمع التبرعات بنموذج دفع" },
  "land.donationbody": { en: "Publish a pay-what-you-want form: RM1.00 minimum, every Malaysian payment method.", ms: "Terbitkan borang bayar-ikut-suka: minimum RM1.00, semua kaedah bayaran Malaysia.", zh: "发布随意支付表单：最低 RM1.00，覆盖所有马来西亚支付方式。", ta: "விருப்பத் தொகைப் படிவம்: குறைந்தபட்சம் RM1.00, மலேசியாவின் அனைத்து கட்டண முறைகளும்.", ar: "انشر نموذج «ادفع ما تشاء»: بحد أدنى RM1.00 وكل وسائل الدفع الماليزية." },
  "land.donation1": { en: "Card, FPX, DuitNow QR", ms: "Kad, FPX, DuitNow QR", zh: "卡、FPX、DuitNow QR", ta: "கார்டு, FPX, DuitNow QR", ar: "بطاقة وFPX وDuitNow QR" },
  "land.donation2": { en: "Donor details in Sales", ms: "Butiran penderma di Jualan", zh: "捐款人资料在销售", ta: "நன்கொடையாளர் விற்பனையில்", ar: "بيانات المتبرع في المبيعات" },
  "land.donation3": { en: "One filter, today's total", ms: "Satu tapisan, jumlah hari ini", zh: "一个筛选看今日总额", ta: "இன்றைய மொத்தம் ஒரு வடிகட்டி", ar: "مرشح واحد لمجموع اليوم" },
  "land.donation4": { en: "Publish in minutes", ms: "Terbit dalam minit", zh: "几分钟内发布", ta: "நிமிடங்களில் வெளியிடு", ar: "انشر في دقائق" },
  "land.donationcta": { en: "Set up donations →", ms: "Sediakan derma →", zh: "设置捐款 →", ta: "நன்கொடை அமைக்க →", ar: "إعداد التبرعات ←" },
  "land.f5": { en: "Affiliate marketing: referral links, automatic attribution and commission tracking. Your AI agent can manage it over MCP.", ms: "Pemasaran affiliate — pautan rujukan, kredit automatik dan pemantauan komisen. Diurus oleh agent AI anda melalui MCP.", zh: "联盟营销 — 推荐链接、自动归属与佣金追踪。通过 MCP 由您的 AI 代理管理。", ta: "இணைப்பு சந்தைப்படுத்தல் — பரிந்துரை இணைப்புகள், தானியங்கி வரவு மற்றும் கமிஷன் கண்காணிப்பு. MCP வழியாக உங்கள் AI முகவரால் நிர்வகிக்கப்படுகிறது.", ar: "التسويق بالعمولة — روابط إحالة ونسبة تلقائية وتتبع العمولة. يُدار عبر وكيل الذكاء الاصطناعي عبر MCP." },
  "mcp.backHome": { en: "← Back to home", ms: "← Balik ke laman utama", zh: "← 返回首页", ta: "← முகப்புக்குத் திரும்பு", ar: "← العودة إلى الرئيسية" },
  "mcp.title": { en: "Connect your AI agent to JomForm (MCP)", ms: "Sambung agent AI kau ke JomForm (MCP)", zh: "将您的 AI 代理连接到 JomForm（MCP）", ta: "உங்கள் AI முகவரை JomForm (MCP) உடன் இணைக்கவும்", ar: "اربط وكيل الذكاء الاصطناعي الخاص بك بـ JomForm (MCP)" },
  "mcp.subtitle": { en: "JomForm speaks the Model Context Protocol. Connect once, then control your whole store with plain language.", ms: "JomForm cakap Model Context Protocol. Sambung sekali, lepas tu kawal seluruh kedai guna bahasa biasa.", zh: "JomForm 支持 MCP。连接一次，即可用自然语言控制整个商店。", ta: "JomForm MCP ஆதரிக்கிறது. ஒருமுறை இணைத்தால் போதும், உங்கள் கடையை வார்த்தைகளால் கட்டுப்படுத்தலாம்.", ar: "يدعم JomForm بروتوكول MCP. اتصل مرة واحدة ثم تحكم بمتجرك بالكامل بلغة بسيطة." },
  "mcp.urlLabel": { en: "Server URL. Paste this exactly into your MCP client", ms: "URL pelayan — tampal tepat ini ke client MCP kau", zh: "服务器地址 — 请将以下内容原样粘贴到您的 MCP 客户端", ta: "சர்வர் URL — உங்கள் MCP கிளையண்டில் இதைச் சரியாக ஒட்டவும்", ar: "عنوان الخادم — الصقه حرفياً في عميل MCP لديك" },
  "mcp.copy": { en: "Copy", ms: "Salin", zh: "复制", ta: "நகலெடு", ar: "نسخ" },
  // The accessible NAME of the configuration snippet's scroll box (card
  // t_804040d3). A horizontal scroller with no tab stop is a keyboard trap
  // (axe `scrollable-region-focusable`, serious), and the fix axe asks for is a
  // FOCUSABLE, NAMED region — a bare tab stop on a <pre> is announced as an
  // unlabelled group, so the name is this key, in the visitor's language.
  "mcp.configExample": { en: "Example configuration", ms: "Contoh konfigurasi", zh: "配置示例", ta: "உள்ளமைவு எடுத்துக்காட்டு", ar: "مثال على الإعداد" },
  "mcp.copied": { en: "Copied ✓", ms: "Disalin ✓", zh: "已复制 ✓", ta: "நகலெடுக்கப்பட்டது ✓", ar: "تم النسخ ✓" },
  "mcp.whatTitle": { en: "What is a connected agent?", ms: "Apa itu agent bersambung?", zh: "什么是已连接的代理？", ta: "இணைக்கப்பட்ட முகவர் என்றால் என்ன?", ar: "ما هو الوكيل المتصل؟" },
  "mcp.whatBody": { en: "A connected agent is your ChatGPT or Claude AI that has authorised JomForm via OAuth 2.1 (one-click, PKCE). Once connected it can read and manage your products, prices, sales, refunds and analytics.", ms: "Agent bersambung ialah AI ChatGPT/Claude kau yang dah benarkan JomForm melalui OAuth 2.1 (satu klik, PKCE). Lepas sambung, ia boleh baca dan urus produk, harga, jualan, refund dan analitik kau — jadi API borang paling pantas untuk kedai kau.", zh: "已连接的代理是通过 OAuth 2.1（一键、PKCE）授权 JomForm 的 AI。连接后可管理您的商品、价格、销售、退款和分析。", ta: "இணைக்கப்பட்ட முகவர் என்பது OAuth 2.1 (ஒரே கிளிக், PKCE) மூலம் JomForm-க்கு அனுமதி அளித்த AI ஆகும். இணைந்ததும் உங்கள் தயாரிப்புகள், விற்பனை, பணத்திருப்பம் ஆகியவற்றை நிர்வகிக்கலாம்.", ar: "الوكيل المتصل هو ذكاء اصطناعي فوض JomForm عبر OAuth 2.1 (نقرة واحدة، PKCE). بمجرد الاتصال يمكنه إدارة منتجاتك وأسعارك ومبيعاتك." },
  "mcp.stepsTitle": { en: "How it works", ms: "Macam mana ia berfungsi", zh: "运作方式", ta: "எப்படி இயங்கும்", ar: "كيف يعمل" },
  "mcp.step1t": { en: "Add the server", ms: "Tambah pelayan", zh: "添加服务器", ta: "சர்வரைச் சேர்க்க", ar: "أضف الخادم" },
  "mcp.step1b": { en: "In your client's MCP server config, paste the full URL: https://mcp.jomform.com/mcp. The /mcp path is required.", ms: "Dalam konfigurasi MCP client kau, tampal URL penuh — https://mcp.jomform.com/mcp. Laluan /mcp wajib.", zh: "在客户端的 MCP 服务器配置中粘贴完整 URL — https://mcp.jomform.com/mcp。必须包含 /mcp 路径。", ta: "உங்கள் கிளையண்டின் MCP சர்வர் கட்டமைப்பில் முழு URL-ஐ ஒட்டவும் — https://mcp.jomform.com/mcp. /mcp பாதை அவசியம்.", ar: "الصق عنوان URL الكامل في إعداد خادم MCP — https://mcp.jomform.com/mcp. مسار /mcp مطلوب." },
  "mcp.step2t": { en: "Connect", ms: "Sambung", zh: "连接", ta: "இணைக்க", ar: "اتصل" },
  "mcp.step2b": { en: "The client fetches the server and shows a JomForm login/authorize page.", ms: "Client dapatkan pelayan dan paparkan halaman log masuk/benarkan JomForm.", zh: "客户端获取服务器并显示 JomForm 登录/授权页面。", ta: "கிளையண்ட் சர்வரைப் பெற்று JomForm உள்நுழைவு/அனுமதி பக்கத்தைக் காட்டும்.", ar: "يجلب العميل الخادم ويعرض صفحة تسجيل الدخول/الترخيص في JomForm." },
  "mcp.step3t": { en: "Authorize & grant", ms: "Benarkan & beri akses", zh: "授权", ta: "அனுமதி", ar: "ترخيص" },
  "mcp.step3b": { en: "Log in and grant access. Your agent is now connected and can act on your store.", ms: "Log masuk dan beri akses. Agent kau dah sambung dan boleh urus kedai kau.", zh: "登录并授予访问权限。您的代理现已连接并可操作您的商店。", ta: "உள்நுழைந்து அனுமதி வழங்கவும். உங்கள் முகவர் இணைக்கப்பட்டு கடையை இயக்கலாம்.", ar: "سجّل الدخول وامنح الإذن. أصبح وكيلك متصلاً ويمكنه التصرف في متجرك." },
  "mcp.step4t": { en: "Done", ms: "Siap", zh: "完成", ta: "முடிந்தது", ar: "تم" },
  "mcp.step4b": { en: "State what you want in plain words. For example: \"add product RM25.00\", \"how many sales today?\", \"refund the last sale\".", ms: "Tiada kunci API, tiada token nak rotate. Cukup cakap: \"tambah produk RM25\", \"berapa sales hari ni?\", \"refund jualan JF-xxxx\".", zh: "无需 API 密钥或轮换令牌。直接说：“添加 25 元商品”、“今天多少销售额？”、“退款销售 JF-xxxx”。", ta: "API விசைகளும் இல்லை, டோக்கன்களும் இல்லை. பேசுங்கள்: \"RM25 தயாரிப்பு சேர்\", \"இன்று எத்தனை விற்பனை?\", \"JF-xxxx விற்பனையை பணம் திருப்பு\".", ar: "لا مفاتيح API ولا توكنات. ابدأ الحديث فحسب." },
  "mcp.clientsTitle": { en: "Per-client examples", ms: "Contoh setiap client", zh: "各客户端的示例", ta: "ஒவ்வொரு கிளையண்டிற்கான எடுத்துக்காட்டுகள்", ar: "أمثلة لكل عميل" },
  "mcp.claudeTitle": { en: "Claude / Claude Desktop", ms: "Claude / Claude Desktop", zh: "Claude / Claude Desktop", ta: "Claude / Claude Desktop", ar: "Claude / Claude Desktop" },
  "mcp.claudeIntro": { en: "Claude Desktop reads its MCP servers from claude_desktop_config.json. Add this entry:", ms: "Claude Desktop baca pelayan MCP dari claude_desktop_config.json. Tambah entri ini:", zh: "Claude Desktop 从 claude_desktop_config.json 读取 MCP 服务器。添加以下条目：", ta: "Claude Desktop, claude_desktop_config.json-ல் இருந்து MCP கட்டமைப்பைப் படிக்கும். இந்த பதிவைச் சேர்க்கவும்:", ar: "يقرأ Claude Desktop خوادم MCP من claude_desktop_config.json. أضف هذا الإدخال:" },
  "mcp.genericTitle": { en: "Other MCP clients", ms: "Client MCP lain", zh: "其他 MCP 客户端", ta: "பிற MCP கிளையண்டுகள்", ar: "عملاء MCP آخرون" },
  "mcp.genericIntro": { en: "Put the server URL into whatever the client calls an MCP server / remote MCP connection. It performs dynamic client registration via OAuth 2.1 (PKCE) automatically.", ms: "Letak URL pelayan ke dalam apa sahaja client panggil pelayan MCP / sambungan MCP remote. Client daftar secara dinamik melalui OAuth 2.1 (PKCE) automatik.", zh: "将服务器地址填入客户端定义的 MCP 服务器/远程连接。它会通过 OAuth 2.1（PKCE）自动进行动态客户端注册。", ta: "MCP சர்வர்/ரிமோட் இணைப்பு என்று அழைக்கப்படும் இடத்தில் URL-ஐ வைக்கவும். OAuth 2.1 (PKCE) மூலம் தானாக பதிவு செய்யும்.", ar: "ضع عنوان الخادم في مكان اتصال العميل بالخادم. يسجّل تلقائياً عبر OAuth 2.1 (PKCE)." },
  "mcp.examplesTitle": { en: "Example commands", ms: "Contoh arahan", zh: "示例命令", ta: "எடுத்துக்காட்டு கட்டளைகள்", ar: "أوامر مثال" },
  "mcp.examplesBody": { en: "State what you want in plain words. For example: \"add product RM25.00\", \"how many sales today?\", \"refund sale JF-xxxx\".", ms: "Lepas sambung, cuba: \"tambah produk RM25\", \"berapa sales hari ni?\", \"refund jualan JF-xxxx\", \"tunjuk stok produk aku\".", zh: "连接后尝试：“添加 25 元商品”、“今天有多少销售？”、“退款销售 JF-xxxx”、“显示我的商品库存”。", ta: "இணைந்த பிறகு முயற்சிக்கவும்: \"RM25 தயாரிப்பைச் சேர்\", \"இன்று எத்தனை விற்பனை?\", \"JF-xxxx விற்பனையை பணம் திருப்பு\", \"என் இருப்பைக் காட்டு\".", ar: "بعد الاتصال جرّب: \"أضف منتجاً بـ 25 ريالاً\" أو تحقق من مبيعات اليوم أو أعد أموال طلب." },
  "mcp.troubleTitle": { en: "Troubleshooting", ms: "Troubleshooting", zh: "故障排除", ta: "சரிசெய்தல்", ar: "استكشاف الأخطاء" },
  "mcp.troubleBare": { en: "Pasting just mcp.jomform.com (no scheme/path) fails. You MUST include https:// and the /mcp path.", ms: "Tampal mcp.jomform.com sahaja (tanpa skema/laluan) gagal. Kena letak https:// dan laluan /mcp.", zh: "仅粘贴 mcp.jomform.com（无协议/路径）会失败。必须包含 https:// 和 /mcp 路径。", ta: "mcp.jomform.com (திட்டம்/பாதை இல்லாமல்) ஒட்டினால் தோல்வி. https:// மற்றும் /mcp பாதை அவசியம்.", ar: "لصق mcp.jomform.com فقط يفشل. يجب تضمين https:// والمسار /mcp." },
  "mcp.troubleConnect": { en: "If connection fails, ensure your client allows remote MCP servers and HTTP-based OAuth. Discovery: the server advertises protected-resource metadata at /.well-known/oauth-protected-resource and authorization-server metadata at /.well-known/oauth-authorization-server. An unauthenticated call to /mcp replies 401 with a WWW-Authenticate header pointing at the protected-resource metadata.", ms: "Kalau sambungan gagal, pastikan client kau benarkan pelayan MCP remote dan OAuth berasaskan HTTP. Discovery: pelayan iklankan protected-resource metadata di /.well-known/oauth-protected-resource dan authorization-server metadata di /.well-known/oauth-authorization-server. Panggilan /mcp tanpa token balas 401 dengan header WWW-Authenticate yang menunjuk ke protected-resource metadata.", zh: "如果连接失败，请确保客户端允许远程 MCP 服务器和基于 HTTP 的 OAuth。发现：服务器在 /.well-known/oauth-protected-resource 提供受保护资源元数据，在 /.well-known/oauth-authorization-server 提供授权服务器元数据。未认证的 /mcp 请求返回 401，并带指向受保护资源元数据的 WWW-Authenticate 头。", ta: "இணைப்பு தோல்வியுற்றால், உங்கள் கிளையண்ட் ரிமோட் MCP சர்வர்களையும் HTTP OAuth-ஐயும் அனுமதிக்கிறதா எனப் பார்க்கவும். Discovery: சர்வர் /.well-known/oauth-protected-resource-இல் protected-resource metadata-ஐயும் /.well-known/oauth-authorization-server-இல் authorization-server metadata-ஐயும் அறிவிக்கிறது. டோக்கன் இல்லாத /mcp அழைப்பு 401 உடன் WWW-Authenticate header திருப்பும்.", ar: "إذا فشل الاتصال فتأكد أن عميلك يسمح بخوادم MCP البعيدة وOAuth. الاكتشاف: الخادم يعلن بيانات المورد المحمي في /.well-known/oauth-protected-resource وبيانات خادم الترخيص في /.well-known/oauth-authorization-server. طلب /mcp بدون توكن يعيد 401 مع رأس WWW-Authenticate." },
  // The scope choice. Two scopes, and which one an agent gets is the client's
  // to ask for at authorize time; the grant is enforced on every write tool.
  "mcp.scopesTitle": { en: "Read-only or read & write", ms: "Baca sahaja atau baca & tulis", zh: "只读或读写", ta: "படிக்க மட்டும் அல்லது படிக்க & எழுத", ar: "للقراءة فقط أو للقراءة والكتابة" },
  "mcp.scopesBody": { en: "A connected agent gets one of two scopes. Choose it in your MCP client when you connect: the client asks JomForm for the scope it wants, and the grant it receives is enforced on every tool call.", ms: "Agent bersambung dapat satu daripada dua skop. Pilih dalam client MCP kau masa sambung — client minta skop yang dia mahu daripada JomForm, dan akses yang diterima dikuatkuasakan pada setiap panggilan tool.", zh: "已连接的代理会获得两种范围之一。连接时在您的 MCP 客户端中选择 — 客户端会向 JomForm 请求所需范围，且授予的权限会在每次工具调用时强制执行。", ta: "இணைக்கப்பட்ட முகவருக்கு இரண்டு scope-களில் ஒன்று கிடைக்கும். இணைக்கும்போது உங்கள் MCP கிளையண்டில் தேர்வுசெய்யவும் — கிளையண்ட் தேவையான scope-ஐ JomForm-இடம் கேட்கும், கிடைக்கும் அனுமதி ஒவ்வொரு tool அழைப்பிலும் அமல்படுத்தப்படும்.", ar: "يحصل الوكيل المتصل على أحد نطاقين. اختره في عميل MCP عند الاتصال — يطلب العميل النطاق الذي يريده من JomForm، ويُفرض الإذن الممنوح عند كل استدعاء أداة." },
  "mcp.scopeRead": { en: "Read-only", ms: "Baca sahaja", zh: "只读", ta: "படிக்க மட்டும்", ar: "للقراءة فقط" },
  "mcp.scopeReadBody": { en: "The agent can look at everything (forms, products, sales, analytics) and change nothing. Every tool that creates, updates, deletes, archives, cancels or refunds is refused with a scope error.", ms: "Agent boleh lihat semuanya — borang, produk, jualan, analitik — tapi tak boleh ubah apa-apa. Setiap tool yang cipta, kemas kini, padam, arkib, batal atau refund ditolak dengan ralat skop.", zh: "代理可以查看所有内容 — 表单、商品、销售、分析 — 但不能更改任何内容。任何创建、更新、删除、归档、取消或退款的工具都会以范围错误被拒绝。", ta: "முகவர் அனைத்தையும் பார்க்கலாம் — படிவங்கள், தயாரிப்புகள், விற்பனை, பகுப்பாய்வு — ஆனால் எதையும் மாற்ற முடியாது. உருவாக்கும், புதுப்பிக்கும், நீக்கும், காப்பகப்படுத்தும், ரத்து செய்யும் அல்லது பணம் திருப்பும் ஒவ்வொரு tool-ம் scope பிழையுடன் மறுக்கப்படும்.", ar: "يمكن للوكيل الاطلاع على كل شيء — النماذج والمنتجات والمبيعات والتحليلات — دون تغيير أي شيء. كل أداة تُنشئ أو تحدّث أو تحذف أو تؤرشف أو تلغي أو تعيد مبلغاً تُرفض بخطأ نطاق." },
  "mcp.scopeReadWrite": { en: "Read & write", ms: "Baca & tulis", zh: "读写", ta: "படிக்க & எழுத", ar: "قراءة وكتابة" },
  "mcp.scopeReadWriteBody": { en: "The agent can read and also change your store: add products, edit forms, refund a sale. This is what a client receives when it does not ask for a scope.", ms: "Agent boleh baca dan juga ubah kedai kau — tambah produk, edit borang, refund jualan. Inilah yang client dapat bila dia tak minta skop.", zh: "代理可以读取，也可以更改您的商店 — 添加商品、编辑表单、退款。客户端未请求范围时即获得此权限。", ta: "முகவர் படிக்கவும், உங்கள் கடையை மாற்றவும் முடியும் — தயாரிப்புகளைச் சேர், படிவங்களைத் திருத்து, விற்பனையை பணம் திருப்பு. scope கேட்காத கிளையண்டுக்கு இதுவே கிடைக்கும்.", ar: "يمكن للوكيل القراءة وتغيير متجرك أيضاً — إضافة منتجات وتعديل النماذج وإعادة مبالغ. وهذا ما يحصل عليه العميل عندما لا يطلب نطاقاً." },
  "mcp.scopeReadOnlyNote": { en: "A read-only grant cannot upgrade itself: it is refused on the tool that creates API keys, so it can never mint a wider credential.", ms: "Akses baca sahaja tak boleh naik taraf sendiri: ia ditolak pada tool yang cipta API key, jadi ia tak boleh hasilkan kredensial yang lebih luas.", zh: "只读授权无法自我升级：创建 API 密钥的工具会拒绝它，因此它永远无法生成权限更大的凭据。", ta: "படிக்க-மட்டும் அனுமதி தன்னை மேம்படுத்திக்கொள்ள முடியாது: API விசைகளை உருவாக்கும் tool-இல் அது மறுக்கப்படும், எனவே பெரிய அனுமதியை உருவாக்கவே முடியாது.", ar: "الإذن للقراءة فقط لا يمكنه ترقية نفسه: يُرفض في الأداة التي تُنشئ مفاتيح API، فلا يمكنه أبداً إصدار اعتماد أوسع." },
  // issue #126: dedicated Sales page (per-form filter + deep-link)
  "nav.sales": { en: "Sales", ms: "Jualan", zh: "销售", ta: "விற்பனைகள்", ar: "المبيعات" },
  // A response form's entries are submissions, not sales (#602/#604). Sits
  // beside nav.sales because both label the same per-form link to
  // /sales?form=<id>; only the noun differs. Singular — Wan's wording.
  // NOT dash.submissions: that is "Submissions:" (a stat-card label with a
  // trailing colon).
  "nav.submissions": { en: "Submission", ms: "Submisi", zh: "提交", ta: "சமர்ப்பிப்பு", ar: "الإرسال" },
  "nav.spam": { en: "Spam", ms: "Spam", zh: "垃圾邮件", ta: "ஸ்பேம்", ar: "رسائل مزعجة" },
  "sales.title": { en: "Sales", ms: "Jualan", zh: "销售", ta: "விற்பனைகள்", ar: "المبيعات" },
  // The capture-amount pre-check's refusal: the amount the merchant typed into
  // the prompt is not a positive number. It was a hardcoded Malay sentence while
  // every other message in that handler was already translated.
  "sales.captureInvalid": { en: "Enter a capture amount above zero, or leave it empty to capture the full amount.", ms: "Masukkan jumlah capture melebihi sifar, atau biarkan kosong untuk capture jumlah penuh.", zh: "请输入大于零的捕获金额，或留空以捕获全部金额。", ta: "பூஜ்ஜியத்திற்கு மேலான கேப்சர் தொகையை உள்ளிடவும், அல்லது முழுத் தொகையையும் கேப்சர் செய்ய காலியாக விடவும்.", ar: "أدخل مبلغ تحصيل أكبر من صفر، أو اتركه فارغاً لتحصيل المبلغ كاملاً." },
  "sales.confirmFail": { en: "Failed to confirm this sale.", ms: "Gagal sahkan jualan ini.", zh: "确认该笔销售失败。", ta: "இந்த விற்பனையை உறுதிப்படுத்த முடியவில்லை.", ar: "فشل تأكيد عملية البيع هذه." },
  "sales.subtitle": { en: "Every sale across your forms. Filter, search, export.", ms: "Semua jualan merentas borang anda — tapis, cari, eksport.", zh: "所有表单的每笔销售 — 筛选、搜索、导出。", ta: "உங்கள் படிவங்களின் ஒவ்வொரு விற்பனையும் — வடிகட்டி, தேடு, ஏற்றுமதி.", ar: "كل مبيعاتك عبر النماذج — صفِّ، ابحث، صدّر." },
  // The unfiltered list is the account's whole record set: sales AND the
  // responses its response-mode forms recorded. Naming it plainly is the point —
  // a merchant who filtered nothing must not be told the page holds only sales.
  "sales.mixedSubtitle": { en: "Every sale and response across your forms. Filter, search, export.", ms: "Setiap jualan dan respons merentas borang anda — tapis, cari, eksport.", zh: "所有表单的每笔销售和响应 — 筛选、搜索、导出。", ta: "உங்கள் படிவங்களின் ஒவ்வொரு விற்பனையும் பதிலும் — வடிகட்டி, தேடு, ஏற்றுமதி.", ar: "كل مبيعة ورد عبر نماذجك — صفِّ، ابحث، صدّر." },
  "sales.type": { en: "Type / Ref", ms: "Jenis / Rujukan", zh: "类型 / 参考号", ta: "வகை / குறிப்பு", ar: "النوع / المرجع" },
  "sales.itemsOrAnswers": { en: "Items / Answers", ms: "Item / Jawapan", zh: "商品 / 回答", ta: "பொருட்கள் / பதில்கள்", ar: "العناصر / الإجابات" },
  "sales.search": { en: "Search ref or buyer…", ms: "Cari rujukan atau pembeli…", zh: "搜索参考号或买家…", ta: "குறிப்பு அல்லது வாங்குபவரைத் தேடு…", ar: "ابحث عن المرجع أو المشتري…" },
  "sales.allForms": { en: "All forms", ms: "Semua borang", zh: "所有表单", ta: "அனைத்து படிவங்களும்", ar: "جميع النماذج" },
  "sales.filterForm": { en: "Form filter", ms: "Tapis borang", zh: "表单筛选", ta: "படிவ வடிப்பான்", ar: "تصفية النموذج" },
  "sales.filterStatus": { en: "Status filter", ms: "Tapis status", zh: "状态筛选", ta: "நிலை வடிப்பான்", ar: "تصفية الحالة" },
  "sales.allStatuses": { en: "All statuses", ms: "Semua status", zh: "所有状态", ta: "அனைத்து நிலைகளும்", ar: "كل الحالات" },
  "sales.from": { en: "From", ms: "Dari", zh: "起始", ta: "இருந்து", ar: "من" },
  "sales.to": { en: "To", ms: "Hingga", zh: "结束", ta: "வரை", ar: "إلى" },
  "sales.dateFrom": { en: "Date from", ms: "Tarikh dari", zh: "起始日期", ta: "தொடக்க தேதி", ar: "من تاريخ" },
  "sales.dateTo": { en: "Date to", ms: "Tarikh hingga", zh: "结束日期", ta: "கடைசி தேதி", ar: "إلى تاريخ" },
  "sales.items": { en: "Items", ms: "Item", zh: "商品", ta: "பொருட்கள்", ar: "العناصر" },
  "sales.form": { en: "Form", ms: "Borang", zh: "表单", ta: "படிவம்", ar: "النموذج" },
  "sales.empty": { en: "No sales match your filters.", ms: "Tiada jualan sepadan tapisan anda.", zh: "没有符合筛选条件的销售。", ta: "உங்கள் வடிப்பான்களுடன் பொருந்தும் விற்பனை இல்லை.", ar: "لا مبيعات تطابق عوامل التصفية." },
  "sales.loading": { en: "Loading sales…", ms: "Memuat jualan…", zh: "加载销售…", ta: "விற்பனைகளை ஏற்றுகிறது…", ar: "جارٍ تحميل المبيعات…" },
  "sales.detail": { en: "Sale detail", ms: "Butiran jualan", zh: "销售详情", ta: "விற்பனை விவரம்", ar: "تفاصيل المبيعات" },
  "sales.close": { en: "Close", ms: "Tutup", zh: "关闭", ta: "மூடு", ar: "إغلاق" },
  "sales.method": { en: "Payment method", ms: "Kaedah bayaran", zh: "支付方式", ta: "கட்டண முறை", ar: "طريقة الدفع" },
  "sales.subtotal": { en: "Subtotal", ms: "Subjumlah", zh: "小计", ta: "தொகை", ar: "المجموع الفرعي" },
  "sales.shipping": { en: "Shipping", ms: "Penghantaran", zh: "运费", ta: "ஷிப்பிங்", ar: "الشحن" },
  // Shipping phase 2: which SOURCE priced the delivery fee, shown under the
  // shipping line in the sale drawer so a disputed fee is explainable from the
  // sale itself. The parenthetical carries the provider or the matched zone name.
  "sales.shipByCourier": { en: "Quoted by a courier at checkout", ms: "Disebut harga oleh kurier semasa bayar", zh: "结账时由快递实时报价", ta: "பணம் செலுத்தும்போது கூரியர் மதிப்பீடு", ar: "عرض سعر من شركة الشحن عند الدفع" },
  "sales.shipByZone": { en: "Charged from your zone rate", ms: "Dikenakan dari kadar zon anda", zh: "按您的区域费率收费", ta: "உங்கள் மண்டலக் கட்டணத்தில் வசூலிக்கப்பட்டது", ar: "محتسب من سعر منطقتك" },
  "sales.shipCourierFellBack": { en: "courier quote did not arrive, zone rate charged instead", ms: "sebut harga kurier tidak masuk, kadar zon dikenakan", zh: "快递未返回报价，改按区域费率收费", ta: "கூரியர் மதிப்பீடு வரவில்லை, மண்டலக் கட்டணம் வசூலிக்கப்பட்டது", ar: "لم يصل عرض سعر الشركة، وتم احتساب سعر المنطقة بدلاً منه" },
  "sales.discount": { en: "Discount", ms: "Diskaun", zh: "折扣", ta: "தள்ளுபடி", ar: "الخصم" },
  "sales.total": { en: "Total", ms: "Jumlah", zh: "合计", ta: "மொத்தம்", ar: "الإجمالي" },
  // Card t_4ca2da1f: the buyer-borne fee and what the merchant nets.
  // `sales.fee` is worded EXACTLY as the buyer's checkout row and the receipt's
  // own line in every locale (api/internal/httpapi/formLang.go FeeLine), so the
  // merchant and the buyer never see one charge called two things; the rate
  // beside it is the API's frozen fee_rate_label. `sales.feeNet` is the drawer's
  // net figure — the same words as the /docs/payment-fees gross-up table header.
  // Pinned together by web/src/lib/salesFee.test.ts.
  //
  // Card t_1a17d22d renamed the charge: it is JomForm's own platform fee, not the
  // acquirer's processing charge, so this key carries the same five words as
  // `dash.fee` and the Go FeeLine (the merge of #1711 with #1702 reconciled them;
  // #1702 wrote this key against the pre-rename wording).
  "sales.fee": { en: "Platform fee", ms: "Caj platform", zh: "平台费", ta: "தளக் கட்டணம்", ar: "رسوم المنصة" },
  "sales.feeNet": { en: "You receive", ms: "Anda terima", zh: "您收到", ta: "நீங்கள் பெறுவது", ar: "تستلم" },
  "sales.feeDocs": { en: "What the platform fee is and how the total is calculated", ms: "Apa itu caj platform dan bagaimana jumlah dikira", zh: "什么是平台费以及总额如何计算", ta: "தளக் கட்டணம் என்ன, மொத்தம் எப்படிக் கணக்கிடப்படுகிறது", ar: "ما رسوم المنصة وكيف يُحتسب الإجمالي" },
  "sales.buyerInfo": { en: "Buyer", ms: "Pembeli", zh: "买家信息", ta: "வாங்குபவர்", ar: "المشتري" },
  "sales.qty": { en: "Qty", ms: "Kuantiti", zh: "数量", ta: "அளவு", ar: "الكمية" },
  "sales.actions": { en: "Actions", ms: "Tindakan", zh: "操作", ta: "செயல்கள்", ar: "الإجراءات" },
  "sales.all": { en: "All", ms: "Semua", zh: "全部", ta: "அனைத்தும்", ar: "الكل" },
  "sales.captureAll": { en: "Capture all pending", ms: "Capture semua menunggu", zh: "捕获全部待处理", ta: "காத்திருப்பன அனைத்தையும் கேப்சர்", ar: "تحصيل الكل المعلق" },
  "sales.confirmSale": { en: "Confirm", ms: "Sahkan", zh: "确认", ta: "உறுதிப்படுத்து", ar: "تأكيد" },
  "sales.refund": { en: "Refund", ms: "Refund", zh: "退款", ta: "பணத்திருப்பம்", ar: "استرداد" },
  "sales.refundInvalid": { en: "Enter a refund above zero and no more than the remaining balance, or leave it empty for a full refund.", ms: "Masukkan refund melebihi sifar dan tidak melebihi baki, atau biarkan kosong untuk refund penuh.", zh: "请输入大于零且不超过剩余余额的退款金额，或留空以全额退款。", ta: "பூஜ்ஜியத்திற்கு மேலும் மீதமுள்ள இருப்புக்கு மிகாமலும் ஒரு பணத்திருப்பத் தொகையை உள்ளிடவும், அல்லது முழுப் பணத்திருப்பத்திற்கு காலியாக விடவும்.", ar: "أدخل مبلغ استرداد أكبر من صفر ولا يتجاوز الرصيد المتبقي، أو اتركه فارغاً لاسترداد كامل." },
  "sales.partiallyRefunded": { en: "Partially refunded", ms: "Refund sebahagian", zh: "部分退款", ta: "பகுதி பணத்திருப்பம்", ar: "مسترد جزئياً" },
  "sales.refundedLabel": { en: "Refunded", ms: "Telah refund", zh: "已退款", ta: "திருப்பப்பட்டது", ar: "المسترد" },
  "sales.outstanding": { en: "Balance", ms: "Baki", zh: "余额", ta: "மீதம்", ar: "الرصيد" },
  "sales.refundBalance": { en: "Refunded so far", ms: "Telah direfund setakat ini", zh: "已退款金额", ta: "இதுவரை திருப்பப்பட்டது", ar: "المسترد حتى الآن" },
  "sales.refundPartialHelp": { en: "Only part of this sale has been refunded. Refund the remaining balance to complete it.", ms: "Sebahagian sahaja jualan ini telah direfund. Refund baki selebihnya untuk melengkapkannya.", zh: "此销售只退款了一部分。退还剩余余额即可完成退款。", ta: "இந்த விற்பனையில் ஒரு பகுதி மட்டுமே திருப்பப்பட்டது. மீதமுள்ள தொகையை திருப்பி முடிக்கவும்.", ar: "تم استرداد جزء فقط من هذه المبيعة. استرد الرصيد المتبقي لإكمال الاسترداد." },
  "sales.viewDetail": { en: "View", ms: "Lihat", zh: "查看", ta: "பார்க்க", ar: "عرض" },
  "sales.payment": { en: "Payment", ms: "Bayaran", zh: "支付", ta: "கட்டணம்", ar: "الدفع" },
  "sales.statusHistory": { en: "Timeline", ms: "Garis masa", zh: "时间线", ta: "காலவரிசை", ar: "الخط الزمني" },
  "sales.saleType": { en: "Sale type", ms: "Jenis jualan", zh: "销售类型", ta: "விற்பனை வகை", ar: "نوع البيع" },
  // Issue #438: QR ordering flow — table number on a sale.
  "sales.table": { en: "Table", ms: "Meja", zh: "桌号", ta: "மேசை", ar: "طاولة" },
  "sales.unauthorized": { en: "Unauthorized (skip-capture, waiting for capture)", ms: "Belum authorized (skip-capture, menunggu capture)", zh: "未捕获（跳过捕获，等待捕获）", ta: "அங்கீகரிக்கப்படாதது (skip-capture, கேப்சருக்காக காத்திருக்கிறது)", ar: "غير مأذون (تخطي التحصيل، بانتظار التحصيل)" },
  "sales.authorized": { en: "Authorized · captured", ms: "Authorized · dicaj", zh: "已授权 · 已捕获", ta: "அங்கீகரிக்கப்பட்டது · கேப்சர்", ar: "مأذون · تم التحصيل" },
  "sales.invoiceNo": { en: "Invoice no.", ms: "No. invois", zh: "发票号", ta: "இன்வாய்ஸ் எண்", ar: "رقم الفاتورة" },
  "sales.noItems": { en: "No line items.", ms: "Tiada item baris.", zh: "无订单行项目。", ta: "வரி உருப்படிகள் இல்லை.", ar: "لا بنود." },
  "sales.exportNote": { en: "Exports all sales for the current filters.", ms: "Eksport semua jualan untuk tapisan semasa.", zh: "按当前筛选条件导出全部销售。", ta: "தற்போதைய வடிப்பான்களுக்கான அனைத்து விற்பனைகளையும் ஏற்றுமதி செய்கிறது.", ar: "يصدّر جميع المبيعات وفق عوامل التصفية الحالية." },
  // issue #447: realtime notifications (live stream + audio beep)
  "sales.live": { en: "Live", ms: "Langsung", zh: "实时", ta: "நேரலை", ar: "مباشر" },
  "sales.offline": { en: "Offline", ms: "Luar talian", zh: "离线", ta: "ஆஃப்லைன்", ar: "غير متصل" },
  "sales.liveHint": { en: "Realtime updates are on. New sales and responses appear without refreshing.", ms: "Kemas kini masa-nyata aktif — jualan/respon baharu muncul tanpa muat semula.", zh: "实时更新已开启 — 新销售/响应无需刷新即可出现。", ta: "நேரலை புதுப்பிப்புகள் இயக்கத்தில் — புதிய விற்பனைகள்/பதில்கள் புதுப்பிக்காமல் தோன்றும்.", ar: "التحديثات الفورية مفعّلة — تظهر المبيعات/الردود الجديدة دون تحديث." },
  "sales.soundLabel": { en: "Play a beep when a new order arrives", ms: "Bunyikan beep bila pesanan baharu tiba", zh: "新订单到达时播放提示音", ta: "புதிய ஆர்டர் வரும்போது பீப் ஒலி", ar: "تشغيل صوت تنبيه عند وصول طلب جديد" },
  // Issue #447: the sound setting is stored per WORKSPACE (and written from this
  // page), so these three state it plainly: the read in flight, a rejected
  // write, and the inline "?" doc link.
  "sales.soundLoading": { en: "Loading this workspace's sound setting…", ms: "Memuatkan tetapan bunyi workspace ini…", zh: "正在加载此工作区的提示音设置…", ta: "இந்த பணியிடத்தின் ஒலி அமைப்பை ஏற்றுகிறது…", ar: "جارٍ تحميل إعداد الصوت لهذه المساحة…" },
  "sales.soundSaveFail": { en: "Could not save the sound setting, so it has been switched back. This setting is saved per workspace.", ms: "Tetapan bunyi gagal disimpan — ia telah dipulihkan. Tetapan ini disimpan bagi setiap workspace.", zh: "无法保存提示音设置 — 已恢复原状。此设置按工作区保存。", ta: "ஒலி அமைப்பைச் சேமிக்க முடியவில்லை — அது மீட்டமைக்கப்பட்டது. இந்த அமைப்பு ஒவ்வொரு பணியிடத்திற்கும் தனியாகச் சேமிக்கப்படுகிறது.", ar: "تعذّر حفظ إعداد الصوت — تم إرجاعه. يُحفظ هذا الإعداد لكل مساحة عمل." },
  "sales.soundReadFail": { en: "Sound setting could not be read", ms: "Tetapan bunyi gagal dibaca", zh: "无法读取提示音设置", ta: "ஒலி அமைப்பைப் படிக்க முடியவில்லை", ar: "تعذّرت قراءة إعداد الصوت" },
  "sales.soundDocs": { en: "Why the sound setting is per workspace", ms: "Mengapa tetapan bunyi adalah per workspace", zh: "为什么提示音设置按工作区保存", ta: "ஒலி அமைப்பு ஏன் ஒவ்வொரு பணியிடத்திற்கும் தனி", ar: "لماذا يُحفظ إعداد الصوت لكل مساحة عمل" },
  // issue #194: email delivery status view
  "del.title": { en: "Email deliveries", ms: "Penghantaran emel", zh: "邮件投递", ta: "மின்னஞ்சல் விநியோகங்கள்", ar: "تسليم البريد" },
  "del.subtitle": { en: "Delivery status of every email: delivered, bounce, open, click. Per form or per sale.", ms: "Status penghantaran setiap emel — delivered, bounce, open, click — per borang atau per jualan.", zh: "每封邮件的投递状态 — 已送达、退回、打开、点击 — 按表单或按销售。", ta: "ஒவ்வொரு மின்னஞ்சலின் விநியோக நிலை — வழங்கப்பட்டது, திரும்பியது, திறந்தது, கிளிக் — படிவம் அல்லது விற்பனை வாரியாக.", ar: "حالة تسليم كل بريد — تم التسليم، ارتد، فتح، نقر — لكل نموذج أو عملية بيع." },
  "del.filterForm": { en: "Form filter", ms: "Tapis borang", zh: "表单筛选", ta: "படிவ வடிப்பான்", ar: "تصفية النموذج" },
  "del.allForms": { en: "All forms", ms: "Semua borang", zh: "所有表单", ta: "அனைத்து படிவங்களும்", ar: "جميع النماذج" },
  "del.filterStatus": { en: "Status filter", ms: "Tapis status", zh: "状态筛选", ta: "நிலை வடிப்பான்", ar: "تصفية الحالة" },
  "del.allStatuses": { en: "All statuses", ms: "Semua status", zh: "所有状态", ta: "அனைத்து நிலைகளும்", ar: "كل الحالات" },
  "del.colEmail": { en: "Email", ms: "Emel", zh: "邮件", ta: "மின்னஞ்சல்", ar: "البريد" },
  "del.colEvent": { en: "Event", ms: "Peristiwa", zh: "事件", ta: "நிகழ்வு", ar: "الحدث" },
  "del.colSale": { en: "Sale", ms: "Jualan", zh: "销售", ta: "விற்பனை", ar: "عملية البيع" },
  "del.colForm": { en: "Form", ms: "Borang", zh: "表单", ta: "படிவம்", ar: "النموذج" },
  "del.colTime": { en: "Time", ms: "Masa", zh: "时间", ta: "நேரம்", ar: "الوقت" },
  "del.colDetail": { en: "Detail", ms: "Butiran", zh: "详情", ta: "விவரம்", ar: "التفاصيل" },
  "del.empty": { en: "No email delivery events match your filters.", ms: "Tiada peristiwa penghantaran emel sepadan tapisan anda.", zh: "没有符合筛选条件的邮件投递事件。", ta: "உங்கள் வடிப்பான்களுடன் பொருந்தும் மின்னஞ்சல் விநியோக நிகழ்வுகள் இல்லை.", ar: "لا توجد أحداث تسليم بريد تطابق عوامل التصفية." },
  "del.loading": { en: "Loading deliveries…", ms: "Memuat penghantaran…", zh: "加载投递…", ta: "விநியோகங்களை ஏற்றுகிறது…", ar: "جارٍ تحميل عمليات التسليم…" },
  "del.loadErr": { en: "Failed to load email deliveries.", ms: "Gagal memuat penghantaran emel.", zh: "加载邮件投递失败。", ta: "மின்னஞ்சல் விநியோகங்களை ஏற்ற முடியவில்லை.", ar: "فشل تحميل تسليم البريد." },
  "del.pageOf": { en: "page {page} of {total}", ms: "halaman {page} daripada {total}", zh: "第 {page} 页，共 {total} 页", ta: "பக்கம் {page} / {total}", ar: "صفحة {page} من {total}" },
  "del.countItems": { en: "{n} event(s)", ms: "{n} peristiwa", zh: "{n} 个事件", ta: "{n} நிகழ்வு(கள்)", ar: "{n} حدث" },
  "del.timeline": { en: "Email timeline", ms: "Garis masa emel", zh: "邮件时间线", ta: "மின்னஞ்சல் காலவரிசை", ar: "الخط الزمني للبريد" },
  "del.sent": { en: "Sent", ms: "Dihantar", zh: "已发送", ta: "அனுப்பப்பட்டது", ar: "أُرسل" },
  "del.noEvents": { en: "No delivery events recorded for this sale.", ms: "Tiada peristiwa penghantaran direkod untuk jualan ini.", zh: "此销售没有记录投递事件。", ta: "இந்த விற்பனைக்கு விநியோக நிகழ்வுகள் பதிவு செய்யப்படவில்லை.", ar: "لا توجد أحداث تسليم مسجلة لهذه العملية." },
  "del.bounceType": { en: "Bounce type", ms: "Jenis bounce", zh: "退回类型", ta: "திரும்பு வகை", ar: "نوع الارتداد" },
  "del.permanent": { en: "Permanent", ms: "Kekal", zh: "永久", ta: "நிரந்தரம்", ar: "دائم" },
  "del.transient": { en: "Transient", ms: "Sementara", zh: "暂时", ta: "தற்காலிகம்", ar: "مؤقت" },
  // Card t_eb09b25a: the workspace-scoped Activity page — the audit trail
  // (`GET /v1/rest/events`), which had NO dashboard surface at all before this.
  // `act.title` is also the sidebar label (Shell.tsx navGroups), the same reuse
  // `del.title` and `sub.title` make for their own nav rows.
  "act.title": { en: "Activity", ms: "Aktiviti", zh: "活动", ta: "செயல்பாடு", ar: "النشاط" },
  "act.subtitle": { en: "Every action in this workspace, newest first. Including the ones that deleted something.", ms: "Setiap tindakan yang direkod dalam workspace ini, terbaru dahulu — termasuk yang memadam sesuatu.", zh: "此工作区记录的每一项操作，最新在前 — 包括删除操作。", ta: "இந்த பணியிடத்தில் பதிவுசெய்யப்பட்ட ஒவ்வொரு செயலும், புதியது முதலில் — எதையாவது நீக்கியவை உட்பட.", ar: "كل إجراء مسجَّل في مساحة العمل هذه، الأحدث أولاً — بما في ذلك الإجراءات التي حذفت شيئاً." },
  "act.docs": { en: "How the activity log works", ms: "Cara log aktiviti berfungsi", zh: "活动日志的工作方式", ta: "செயல்பாட்டுப் பதிவு எவ்வாறு செயல்படுகிறது", ar: "كيف يعمل سجل النشاط" },
  // THE HUMAN LABEL FOR EVERY EVENT THE API RECORDS (card t_804040d3). The
  // Activity table used to print the machine token (`sale.paid`,
  // `workspace.renamed`) straight into the merchant's cell. One key per recorded
  // event name, named `act.event.<event name>`; the population is declared in
  // src/lib/activityEvents.ts and pinned against the recorder call sites under
  // api/internal by src/lib/activityPage.test.ts, so a new recorder cannot ship
  // as a raw token in all five locales.
  "act.event.admin.super_admin_action": { en: "Super admin action", ms: "Tindakan super admin", zh: "超级管理员操作", ta: "சூப்பர் அட்மின் செயல்", ar: "إجراء من مشرف أعلى" },
  "act.event.auth.login": { en: "Signed in", ms: "Log masuk", zh: "已登录", ta: "உள்நுழைந்தார்", ar: "تسجيل دخول" },
  "act.event.auth.logout": { en: "Signed out", ms: "Log keluar", zh: "已退出登录", ta: "வெளியேறினார்", ar: "تسجيل خروج" },
  "act.event.auth.stepup_required": { en: "Extra verification needed", ms: "Pengesahan tambahan diperlukan", zh: "需要额外验证", ta: "கூடுதல் சரிபார்ப்பு தேவை", ar: "مطلوب تحقق إضافي" },
  "act.event.chat.message": { en: "Question to the assistant", ms: "Soalan kepada pembantu", zh: "向助手提问", ta: "உதவியாளருக்குக் கேள்வி", ar: "سؤال إلى المساعد" },
  "act.event.chat.proposal_confirmed": { en: "Assistant change confirmed", ms: "Perubahan pembantu disahkan", zh: "已确认助手变更", ta: "உதவியாளர் மாற்றம் உறுதிசெய்யப்பட்டது", ar: "تم تأكيد تغيير المساعد" },
  "act.event.feedback.created": { en: "Feedback sent", ms: "Maklum balas dihantar", zh: "已提交反馈", ta: "கருத்து அனுப்பப்பட்டது", ar: "تم إرسال ملاحظات" },
  "act.event.feedback.replied": { en: "Feedback answered", ms: "Maklum balas dijawab", zh: "已回复反馈", ta: "கருத்துக்குப் பதில் அளிக்கப்பட்டது", ar: "تم الرد على الملاحظات" },
  "act.event.feedback.admin_replied": { en: "Reply from JomForm support", ms: "Balasan daripada sokongan JomForm", zh: "JomForm 支持团队的回复", ta: "JomForm ஆதரவின் பதில்", ar: "رد من دعم JomForm" },
  "act.event.feedback.status_changed": { en: "Feedback status changed", ms: "Status maklum balas berubah", zh: "反馈状态已变更", ta: "கருத்து நிலை மாறியது", ar: "تغيّرت حالة الملاحظات" },
  "act.event.feedback.notify_retried": { en: "Feedback notification re-sent", ms: "Notifikasi maklum balas dihantar semula", zh: "已重新发送反馈通知", ta: "கருத்து அறிவிப்பு மீண்டும் அனுப்பப்பட்டது", ar: "أُعيد إرسال إشعار الملاحظات" },
  "act.event.form.created": { en: "Form created", ms: "Borang dicipta", zh: "已创建表单", ta: "படிவம் உருவாக்கப்பட்டது", ar: "تم إنشاء نموذج" },
  "act.event.form.updated": { en: "Form updated", ms: "Borang dikemas kini", zh: "已更新表单", ta: "படிவம் புதுப்பிக்கப்பட்டது", ar: "تم تحديث نموذج" },
  "act.event.form.deleted": { en: "Form deleted", ms: "Borang dipadam", zh: "已删除表单", ta: "படிவம் நீக்கப்பட்டது", ar: "تم حذف نموذج" },
  "act.event.form.published": { en: "Form published", ms: "Borang diterbitkan", zh: "已发布表单", ta: "படிவம் வெளியிடப்பட்டது", ar: "تم نشر نموذج" },
  "act.event.form.purged": { en: "Form permanently deleted", ms: "Borang dipadam selamanya", zh: "表单已永久删除", ta: "படிவம் நிரந்தரமாக நீக்கப்பட்டது", ar: "تم حذف النموذج نهائياً" },
  "act.event.form.restored": { en: "Form restored", ms: "Borang dipulihkan", zh: "已恢复表单", ta: "படிவம் மீட்டெடுக்கப்பட்டது", ar: "تمت استعادة نموذج" },
  "act.event.form.slug_renamed": { en: "Form link renamed", ms: "Pautan borang dinamakan semula", zh: "表单链接已改名", ta: "படிவ இணைப்பு மறுபெயரிடப்பட்டது", ar: "أُعيدت تسمية رابط النموذج" },
  "act.event.form.bulk_publish": { en: "Several forms published", ms: "Beberapa borang diterbitkan", zh: "已批量发布表单", ta: "பல படிவங்கள் வெளியிடப்பட்டன", ar: "تم نشر عدة نماذج" },
  "act.event.form.bulk_draft": { en: "Several forms moved to draft", ms: "Beberapa borang dipindah ke draf", zh: "多个表单已转为草稿", ta: "பல படிவங்கள் வரைவுக்கு மாற்றப்பட்டன", ar: "نُقلت عدة نماذج إلى المسودة" },
  "act.event.form.bulk_delete": { en: "Several forms deleted", ms: "Beberapa borang dipadam", zh: "已删除多个表单", ta: "பல படிவங்கள் நீக்கப்பட்டன", ar: "حُذفت عدة نماذج" },
  "act.event.form.status_draft": { en: "Form is now a draft", ms: "Borang kini draf", zh: "表单现为草稿", ta: "படிவம் இப்போது வரைவில்", ar: "النموذج الآن مسودة" },
  "act.event.form.status_published": { en: "Form is now published", ms: "Borang kini diterbitkan", zh: "表单现已发布", ta: "படிவம் இப்போது வெளியிடப்பட்டுள்ளது", ar: "النموذج الآن منشور" },
  "act.event.form.status_unlisted": { en: "Form is now unlisted", ms: "Borang kini tidak tersenarai", zh: "表单现为不公开列出", ta: "படிவம் இப்போது பட்டியலில் இல்லை", ar: "النموذج الآن غير مُدرج" },
  "act.event.form.status_private": { en: "Form is now private", ms: "Borang kini peribadi", zh: "表单现为私有", ta: "படிவம் இப்போது தனியுரிமை", ar: "النموذج الآن خاص" },
  // Card t_9a6cc85f: the OAuth scope ladder + Connected apps. Activity rows for
  // a credential change say what moved, because "who widened this" is the
  // question an audit trail exists to answer.
  "act.event.oauth.device_approved": { en: "You approved an AI agent connection from an email code", ms: "Anda meluluskan sambungan ejen AI melalui kod emel", zh: "您通过邮件代码批准了 AI 代理连接", ta: "மின்னஞ்சல் குறியீட்டின் மூலம் AI முகவர் இணைப்பை அனுமதித்தீர்கள்", ar: "وافقت على اتصال وكيل ذكاء اصطناعي عبر رمز بريدي" },
  "act.event.oauth.scope_changed": { en: "You changed an AI agent's access", ms: "Anda mengubah akses ejen AI", zh: "您更改了 AI 代理的权限", ta: "AI முகவரின் அணுகலை மாற்றினீர்கள்", ar: "غيّرت وصول وكيل ذكاء اصطناعي" },
  "act.event.oauth.scope_upgraded": { en: "You gave an AI agent write access", ms: "Anda memberi ejen AI akses tulis", zh: "您授予了 AI 代理写入权限", ta: "AI முகவருக்கு எழுதும் அணுகலை வழங்கினீர்கள்", ar: "منحت وكيل ذكاء اصطناعي صلاحية الكتابة" },
  "act.event.oauth.token_revoked": { en: "You revoked an AI agent connection", ms: "Anda membatalkan sambungan ejen AI", zh: "您撤销了一个 AI 代理连接", ta: "AI முகவர் இணைப்பை ரத்துசெய்தீர்கள்", ar: "ألغيت اتصال وكيل ذكاء اصطناعي" },
  "act.event.mcp.tool_call": { en: "AI agent used a tool", ms: "Ejen AI menggunakan alat", zh: "AI 助手调用了工具", ta: "AI முகவர் ஒரு கருவியைப் பயன்படுத்தியது", ar: "استخدم وكيل الذكاء أداة" },
  "act.event.mcp.agent_disconnected": { en: "AI agent disconnected", ms: "Ejen AI diputuskan", zh: "AI 助手已断开连接", ta: "AI முகவர் துண்டிக்கப்பட்டது", ar: "تم فصل وكيل الذكاء" },
  "act.event.media.deleted": { en: "Media deleted", ms: "Media dipadam", zh: "已删除媒体", ta: "மீடியா நீக்கப்பட்டது", ar: "تم حذف وسائط" },
  "act.event.media.purged": { en: "Media permanently deleted", ms: "Media dipadam selamanya", zh: "媒体已永久删除", ta: "மீடியா நிரந்தரமாக நீக்கப்பட்டது", ar: "تم حذف الوسائط نهائياً" },
  "act.event.media.restored": { en: "Media restored", ms: "Media dipulihkan", zh: "已恢复媒体", ta: "மீடியா மீட்டெடுக்கப்பட்டது", ar: "تمت استعادة الوسائط" },
  "act.event.product.updated": { en: "Product updated", ms: "Produk dikemas kini", zh: "已更新产品", ta: "தயாரிப்பு புதுப்பிக்கப்பட்டது", ar: "تم تحديث منتج" },
  "act.event.product.deleted": { en: "Product deleted", ms: "Produk dipadam", zh: "已删除产品", ta: "தயாரிப்பு நீக்கப்பட்டது", ar: "تم حذف منتج" },
  "act.event.product.purged": { en: "Product permanently deleted", ms: "Produk dipadam selamanya", zh: "产品已永久删除", ta: "தயாரிப்பு நிரந்தரமாக நீக்கப்பட்டது", ar: "تم حذف المنتج نهائياً" },
  "act.event.product.restored": { en: "Product restored", ms: "Produk dipulihkan", zh: "已恢复产品", ta: "தயாரிப்பு மீட்டெடுக்கப்பட்டது", ar: "تمت استعادة منتج" },
  "act.event.product.bulk_archive": { en: "Several products archived", ms: "Beberapa produk diarkibkan", zh: "已归档多个产品", ta: "பல தயாரிப்புகள் காப்பகப்படுத்தப்பட்டன", ar: "أُرشفت عدة منتجات" },
  "act.event.profile.avatar_update": { en: "Profile photo updated", ms: "Gambar profil dikemas kini", zh: "已更新头像", ta: "சுயவிவரப் படம் புதுப்பிக்கப்பட்டது", ar: "تم تحديث صورة الملف" },
  "act.event.sale.captured": { en: "Payment captured", ms: "Bayaran dikutip", zh: "款项已收取", ta: "பணம் வசூலிக்கப்பட்டது", ar: "تم تحصيل الدفعة" },
  "act.event.sale.released": { en: "Payment released", ms: "Bayaran dilepaskan", zh: "款项已释放", ta: "பணம் விடுவிக்கப்பட்டது", ar: "تم تحرير الدفعة" },
  "act.event.sale.refunded": { en: "Sale refunded", ms: "Jualan dikembalikan", zh: "销售已退款", ta: "விற்பனைக்கு பணம் திருப்பப்பட்டது", ar: "تم رد مبلغ البيع" },
  "act.event.sale.confirmed": { en: "Sale confirmed", ms: "Jualan disahkan", zh: "销售已确认", ta: "விற்பனை உறுதிசெய்யப்பட்டது", ar: "تم تأكيد البيع" },
  "act.event.sale.bulk_captured": { en: "Several payments captured", ms: "Beberapa bayaran dikutip", zh: "已批量收款", ta: "பல பணம் வசூலிக்கப்பட்டது", ar: "تم تحصيل عدة دفعات" },
  "act.event.settings.api_key_created": { en: "API key created", ms: "Kunci API dicipta", zh: "已创建 API 密钥", ta: "API விசை உருவாக்கப்பட்டது", ar: "تم إنشاء مفتاح API" },
  "act.event.settings.api_key_revoked": { en: "API key revoked", ms: "Kunci API dibatalkan", zh: "已撤销 API 密钥", ta: "API விசை ரத்து செய்யப்பட்டது", ar: "تم إلغاء مفتاح API" },
  "act.event.settings.chip_cleared": { en: "Payment connection removed", ms: "Sambungan bayaran dibuang", zh: "已移除支付连接", ta: "கட்டண இணைப்பு நீக்கப்பட்டது", ar: "تمت إزالة اتصال الدفع" },
  "act.event.settings.chip_updated": { en: "Payment connection updated", ms: "Sambungan bayaran dikemas kini", zh: "已更新支付连接", ta: "கட்டண இணைப்பு புதுப்பிக்கப்பட்டது", ar: "تم تحديث اتصال الدفع" },
  "act.event.settings.domain_added": { en: "Domain added", ms: "Domain ditambah", zh: "已添加域名", ta: "டொமைன் சேர்க்கப்பட்டது", ar: "تمت إضافة نطاق" },
  "act.event.settings.domain_claimed": { en: "Domain claimed", ms: "Domain dituntut", zh: "域名已认领", ta: "டொமைன் கோரப்பட்டது", ar: "تم إثبات ملكية النطاق" },
  "act.event.settings.domain_cleared": { en: "Domain detached", ms: "Domain dilepaskan", zh: "域名已解绑", ta: "டொமைன் விலக்கப்பட்டது", ar: "تم فصل النطاق" },
  "act.event.settings.domain_custom_cert": { en: "Custom certificate uploaded", ms: "Sijil tersuai dimuat naik", zh: "已上传自定义证书", ta: "தனிப்பயன் சான்றிதழ் பதிவேற்றப்பட்டது", ar: "تم رفع شهادة مخصصة" },
  "act.event.settings.domain_removed": { en: "Domain removed", ms: "Domain dibuang", zh: "已移除域名", ta: "டொமைன் நீக்கப்பட்டது", ar: "تمت إزالة النطاق" },
  "act.event.settings.domain_tls_email": { en: "Certificate email changed", ms: "Emel sijil ditukar", zh: "证书邮箱已更改", ta: "சான்றிதழ் மின்னஞ்சல் மாற்றப்பட்டது", ar: "تم تغيير بريد الشهادة" },
  "act.event.settings.domain_tls_mode": { en: "Certificate mode changed", ms: "Mod sijil ditukar", zh: "证书模式已更改", ta: "சான்றிதழ் பயன்முறை மாற்றப்பட்டது", ar: "تم تغيير وضع الشهادة" },
  "act.event.settings.domain_verified": { en: "Domain verified", ms: "Domain disahkan", zh: "域名已验证", ta: "டொமைன் சரிபார்க்கப்பட்டது", ar: "تم التحقق من النطاق" },
  "act.event.settings.webhook_created": { en: "Webhook added", ms: "Webhook ditambah", zh: "已添加 Webhook", ta: "வெப்ஹூக் சேர்க்கப்பட்டது", ar: "تمت إضافة ويب هوك" },
  "act.event.settings.webhook_updated": { en: "Webhook updated", ms: "Webhook dikemas kini", zh: "已更新 Webhook", ta: "வெப்ஹூக் புதுப்பிக்கப்பட்டது", ar: "تم تحديث ويب هوك" },
  "act.event.settings.webhook_deleted": { en: "Webhook removed", ms: "Webhook dibuang", zh: "已移除 Webhook", ta: "வெப்ஹூக் நீக்கப்பட்டது", ar: "تمت إزالة ويب هوك" },
  "act.event.settings.webhook_retried": { en: "Webhook re-sent", ms: "Webhook dihantar semula", zh: "已重新发送 Webhook", ta: "வெப்ஹூக் மீண்டும் அனுப்பப்பட்டது", ar: "أُعيد إرسال ويب هوك" },
  "act.event.template.created": { en: "Template saved", ms: "Templat disimpan", zh: "已保存模板", ta: "வார்ப்புரு சேமிக்கப்பட்டது", ar: "تم حفظ قالب" },
  "act.event.template.updated": { en: "Template updated", ms: "Templat dikemas kini", zh: "已更新模板", ta: "வார்ப்புரு புதுப்பிக்கப்பட்டது", ar: "تم تحديث قالب" },
  "act.event.template.deleted": { en: "Template deleted", ms: "Templat dipadam", zh: "已删除模板", ta: "வார்ப்புரு நீக்கப்பட்டது", ar: "تم حذف قالب" },
  "act.event.upload.asset": { en: "File uploaded", ms: "Fail dimuat naik", zh: "已上传文件", ta: "கோப்பு பதிவேற்றப்பட்டது", ar: "تم رفع ملف" },
  "act.event.upload.avatar": { en: "Profile photo uploaded", ms: "Gambar profil dimuat naik", zh: "已上传头像", ta: "சுயவிவரப் படம் பதிவேற்றப்பட்டது", ar: "تم رفع صورة الملف" },
  "act.event.upload.image": { en: "Image uploaded", ms: "Imej dimuat naik", zh: "已上传图片", ta: "படம் பதிவேற்றப்பட்டது", ar: "تم رفع صورة" },
  "act.event.upload.pdf": { en: "PDF uploaded", ms: "PDF dimuat naik", zh: "已上传 PDF", ta: "PDF பதிவேற்றப்பட்டது", ar: "تم رفع ملف PDF" },
  "act.event.workspace.deleted": { en: "Workspace deleted", ms: "Ruang kerja dipadam", zh: "已删除工作区", ta: "பணியிடம் நீக்கப்பட்டது", ar: "تم حذف مساحة العمل" },
  "act.event.workspace.purged": { en: "Workspace permanently deleted", ms: "Ruang kerja dipadam selamanya", zh: "工作区已永久删除", ta: "பணியிடம் நிரந்தரமாக நீக்கப்பட்டது", ar: "تم حذف مساحة العمل نهائياً" },
  "act.event.workspace.renamed": { en: "Workspace renamed", ms: "Ruang kerja dinamakan semula", zh: "工作区已改名", ta: "பணியிடம் மறுபெயரிடப்பட்டது", ar: "أُعيدت تسمية مساحة العمل" },
  "act.event.workspace.restored": { en: "Workspace restored", ms: "Ruang kerja dipulihkan", zh: "已恢复工作区", ta: "பணியிடம் மீட்டெடுக்கப்பட்டது", ar: "تمت استعادة مساحة العمل" },
  "act.filterEvent": { en: "Event type", ms: "Jenis peristiwa", zh: "事件类型", ta: "நிகழ்வு வகை", ar: "نوع الحدث" },
  "act.allEvents": { en: "All events", ms: "Semua peristiwa", zh: "所有事件", ta: "அனைத்து நிகழ்வுகளும்", ar: "كل الأحداث" },
  "act.filterFrom": { en: "From", ms: "Dari", zh: "起始", ta: "இருந்து", ar: "من" },
  "act.filterTo": { en: "To", ms: "Hingga", zh: "结束", ta: "வரை", ar: "إلى" },
  "act.colEvent": { en: "Event", ms: "Peristiwa", zh: "事件", ta: "நிகழ்வு", ar: "الحدث" },
  "act.colProperties": { en: "Detail", ms: "Butiran", zh: "详情", ta: "விவரம்", ar: "التفاصيل" },
  "act.colTime": { en: "Time", ms: "Masa", zh: "时间", ta: "நேரம்", ar: "الوقت" },
  "act.empty": { en: "No recorded events match your filters.", ms: "Tiada peristiwa direkod sepadan tapisan anda.", zh: "没有符合筛选条件的已记录事件。", ta: "உங்கள் வடிப்பான்களுடன் பொருந்தும் பதிவுசெய்யப்பட்ட நிகழ்வுகள் இல்லை.", ar: "لا توجد أحداث مسجَّلة تطابق عوامل التصفية." },
  "act.loading": { en: "Loading activity…", ms: "Memuatkan aktiviti…", zh: "正在加载活动…", ta: "செயல்பாட்டை ஏற்றுகிறது…", ar: "جارٍ تحميل النشاط…" },
  "act.loadErr": { en: "Failed to load the activity log.", ms: "Gagal memuat log aktiviti.", zh: "加载活动日志失败。", ta: "செயல்பாட்டுப் பதிவை ஏற்ற முடியவில்லை.", ar: "فشل تحميل سجل النشاط." },
  "act.pageOf": { en: "page {page} of {total}", ms: "halaman {page} daripada {total}", zh: "第 {page} 页，共 {total} 页", ta: "பக்கம் {page} / {total}", ar: "صفحة {page} من {total}" },
  "act.countItems": { en: "{n} event(s)", ms: "{n} peristiwa", zh: "{n} 个事件", ta: "{n} நிகழ்வு(கள்)", ar: "{n} حدث" },
  "act.retention": { en: "Events are kept for 90 days, or 180 days for sign-in and settings changes. The log is append-only: nothing here can be edited or deleted from the dashboard.", ms: "Peristiwa disimpan 90 hari, atau 180 hari untuk log masuk dan perubahan tetapan. Log ini tambah-sahaja: tiada apa di sini boleh diedit atau dipadam dari dashboard.", zh: "事件保留 90 天，登录和设置变更保留 180 天。此日志仅可追加：仪表板中无法编辑或删除任何记录。", ta: "நிகழ்வுகள் 90 நாட்கள், உள்நுழைவு மற்றும் அமைப்பு மாற்றங்களுக்கு 180 நாட்கள் வைக்கப்படும். இந்தப் பதிவு சேர்க்கை-மட்டுமே: இங்குள்ள எதையும் டாஷ்போர்டில் திருத்தவோ நீக்கவோ முடியாது.", ar: "يُحتفظ بالأحداث 90 يوماً، أو 180 يوماً لتسجيل الدخول وتغييرات الإعدادات. هذا السجل للإضافة فقط: لا يمكن تعديل أو حذف أي شيء فيه من لوحة التحكم." },
  "sales.clearFilters": { en: "Clear filters", ms: "Kosongkan tapisan", zh: "清除筛选", ta: "வடிப்பான்களை அழி", ar: "مسح عوامل التصفية" },
  "sales.notFound": { en: "Sale not found.", ms: "Jualan tidak dijumpai.", zh: "未找到销售。", ta: "விற்பனை கிடைக்கவில்லை.", ar: "لم يتم العثور على البيع." },
  // issue #245: AI spam review queue
  "spam.title": { en: "Spam review", ms: "Semakan spam", zh: "垃圾邮件审核", ta: "ஸ்பேம் மதிப்பாய்வு", ar: "مراجعة الرسائل المزعجة" },
  "spam.subtitle": { en: "Submissions and sales the AI judge flagged as spam. Un-spam a false positive; spam entries auto-delete after 30 days.", ms: "Submisi & jualan yang ditanda spam oleh AI. Nyah-spam jika salah; entri spam auto-padam selepas 30 hari.", zh: "AI 判定为垃圾邮件的提交和销售。误判可取消标记；垃圾邮件条目 30 天后自动删除。", ta: "AI நீதிபதி ஸ்பேம் எனக் குறித்த சமர்ப்பிப்புகள் & விற்பனைகள். தவறான நேர்மறையை நீக்கவும்; ஸ்பேம் உள்ளீடுகள் 30 நாட்களுக்குப் பிறகு தானாக நீக்கப்படும்.", ar: "الطلبات والمبيعات التي صنفها الذكاء الاصطناعي كرسائل مزعجة. ألغِ الإشارة للنتائج الإيجابية الخاطئة؛ تُحذف الرسائل المزعجة تلقائياً بعد 30 يوماً." },
  "spam.submissions": { en: "Submissions", ms: "Submisi", zh: "提交", ta: "சமர்ப்பிப்புகள்", ar: "الطلبات" },
  "spam.sales": { en: "Sales", ms: "Jualan", zh: "销售", ta: "விற்பனைகள்", ar: "المبيعات" },
  "spam.empty": { en: "No spam-flagged entries.", ms: "Tiada entri ditanda spam.", zh: "没有垃圾邮件条目。", ta: "ஸ்பேம் குறிக்கப்பட்ட உள்ளீடுகள் இல்லை.", ar: "لا توجد عناصر مميزة كرسائل مزعجة." },
  "spam.reason": { en: "Reason", ms: "Sebab", zh: "原因", ta: "காரணம்", ar: "السبب" },
  "spam.unspam": { en: "Un-spam", ms: "Nyah-spam", zh: "取消标记", ta: "ஸ்பேம் நீக்கு", ar: "إلغاء الإشارة" },
  "spam.unspammed": { en: "Un-spammed ✓", ms: "Nyah-spam ✓", zh: "已取消标记 ✓", ta: "ஸ்பேம் நீக்கப்பட்டது ✓", ar: "تم إلغاء الإشارة ✓" },
  "spam.unspamFail": { en: "Failed to un-spam.", ms: "Gagal nyah-spam.", zh: "取消标记失败。", ta: "ஸ்பேம் நீக்க முடியவில்லை.", ar: "فشل إلغاء الإشارة." },
  "spam.loading": { en: "Loading…", ms: "Memuat…", zh: "加载中…", ta: "ஏற்றுகிறது…", ar: "جارٍ التحميل…" },
  "spam.autoUnspamNote": { en: "A spam-flagged sale that receives a successful payment is automatically un-spammed.", ms: "Jualan ditanda spam yang menerima bayaran berjaya auto-nyah-spam.", zh: "收到成功付款的垃圾邮件标记销售会自动取消标记。", ta: "வெற்றிகரமான கட்டணம் பெறும் ஸ்பேம்-குறிக்கப்பட்ட விற்பனை தானாக ஸ்பேம் நீக்கப்படும்.", ar: "تُلغى الإشارة تلقائياً عن أي بيع مميز كرسالة مزعجة يحصل على دفعة ناجحة." },
  // issue #180: media library (WordPress-style)
  "media.title": { en: "Media", ms: "Media", zh: "媒体", ta: "ஊடகம்", ar: "الوسائط" },
  "media.subtitle": { en: "All images and code files you've uploaded, in one place.", ms: "Semua gambar & fail kod yang anda muat naik, di satu tempat.", zh: "您上传的所有图片和代码文件，集中在一处。", ta: "நீங்கள் பதிவேற்றிய அனைத்து படங்கள் மற்றும் குறியீடு கோப்புகளும், ஒரே இடத்தில்.", ar: "كل الصور وملفات الأكواد التي رفعتها في مكان واحد." },
  "media.empty": { en: "No media yet. Upload an image, code file (CSS/JS/HTML) or PDF above.", ms: "Belum ada media — muat naik gambar, fail kod (CSS/JS/HTML) atau PDF di atas.", zh: "暂无媒体 — 请在上方上传图片、代码文件（CSS/JS/HTML）或 PDF。", ta: "இன்னும் ஊடகம் இல்லை — மேலே படம், குறியீடு கோப்பு (CSS/JS/HTML) அல்லது PDF பதிவேற்றவும்.", ar: "لا وسائط بعد — ارفع صورة أو ملف أكواد (CSS/JS/HTML) أو PDF أعلاه." },
  "media.codeFile": { en: "Code file", ms: "Fail kod", zh: "代码文件", ta: "குறியீடு கோப்பு", ar: "ملف أكواد" },
  "media.countItems": { en: "{n} items", ms: "{n} item", zh: "{n} 项", ta: "{n} உருப்படிகள்", ar: "{n} عنصر" },
  "media.filterForm": { en: "All forms", ms: "Semua borang", zh: "所有表单", ta: "அனைத்து படிவங்கள்", ar: "كل النماذج" },
  "media.filterSource": { en: "All sources", ms: "Semua sumber", zh: "所有来源", ta: "அனைத்து ஆதாரங்கள்", ar: "كل المصادر" },
  "media.sourceMerchant": { en: "Merchant", ms: "Pedagang", zh: "商家", ta: "வணிகர்", ar: "تاجر" },
  "media.sourceCustomer": { en: "Customer", ms: "Pelanggan", zh: "客户", ta: "வாடிக்கையாளர்", ar: "عميل" },
  "media.name": { en: "Name", ms: "Nama", zh: "名称", ta: "பெயர்", ar: "الاسم" },
  "media.date": { en: "Date", ms: "Tarikh", zh: "日期", ta: "தேதி", ar: "التاريخ" },
  "media.size": { en: "Size", ms: "Saiz", zh: "大小", ta: "அளவு", ar: "الحجم" },
  "media.delete": { en: "Delete", ms: "Buang", zh: "删除", ta: "நீக்கு", ar: "حذف" },
  "media.deleteConfirm": { en: "Delete this image? It will be removed from your media library and storage.", ms: "Buang gambar ini? Ia akan dikeluarkan dari perpustakaan media dan storan anda.", zh: "删除此图片？它将从您的媒体库和存储中移除。", ta: "இந்த படத்தை நீக்கவா? இது உங்கள் ஊடக நூலகம் மற்றும் சேமிப்பிலிருந்து அகற்றப்படும்.", ar: "حذف هذه الصورة؟ ستُزال من مكتبة الوسائط والتخزين." },
  "media.deleted": { en: "Deleted ✓", ms: "Dibuang ✓", zh: "已删除 ✓", ta: "நீக்கப்பட்டது ✓", ar: "تم الحذف ✓" },
  "media.deleteFail": { en: "Failed to delete media.", ms: "Gagal buang media.", zh: "删除媒体失败。", ta: "ஊடகத்தை நீக்க முடியவில்லை.", ar: "فشل حذف الوسائط." },
  "media.view": { en: "View", ms: "Lihat", zh: "查看", ta: "பார்", ar: "عرض" },
  // issue #383: PDF preview (auto-generated first-page thumbnail)
  "media.viewPdf": { en: "View PDF", ms: "Lihat PDF", zh: "查看 PDF", ta: "PDF பார்", ar: "عرض PDF" },
  "media.preview": { en: "Preview", ms: "Pratonton", zh: "预览", ta: "முன்னோட்டம்", ar: "معاينة" },
  "media.copyUrl": { en: "Copy URL", ms: "Salin URL", zh: "复制链接", ta: "URL நகலெடு", ar: "نسخ الرابط" },
  "media.copied": { en: "URL copied ✓", ms: "URL disalin ✓", zh: "链接已复制 ✓", ta: "URL நகலெடுக்கப்பட்டது ✓", ar: "تم نسخ الرابط ✓" },
  "media.pageOf": { en: "page {page} of {total}", ms: "halaman {page} daripada {total}", zh: "第 {page} 页，共 {total} 页", ta: "பக்கம் {page} / {total}", ar: "صفحة {page} من {total}" },
  // issue #382: media page upload + multiple image sizes
  "media.addNew": { en: "Add New Media", ms: "Tambah Media Baharu", zh: "添加新媒体", ta: "புதிய ஊடகம் சேர்", ar: "إضافة وسائط جديدة" },
  "media.upload": { en: "Upload", ms: "Muat naik", zh: "上传", ta: "பதிவேற்று", ar: "رفع" },
  "media.uploading": { en: "Uploading…", ms: "Memuat naik…", zh: "上传中…", ta: "பதிவேற்றுகிறது…", ar: "جارٍ الرفع…" },
  "media.uploadFail": { en: "Upload failed.", ms: "Muat naik gagal.", zh: "上传失败。", ta: "பதிவேற்றம் தோல்வி.", ar: "فشل الرفع." },
  "media.uploaded": { en: "Uploaded ✓", ms: "Dimuat naik ✓", zh: "已上传 ✓", ta: "பதிவேற்றப்பட்டது ✓", ar: "تم الرفع ✓" },
  "media.dropHint": { en: "Drag & drop files here, or click to browse", ms: "Seret & lepas fail di sini, atau klik untuk pilih", zh: "将文件拖放到此处，或点击浏览", ta: "கோப்புகளை இங்கே இழுத்து விடுங்கள், அல்லது உலாவ கிளிக் செய்யவும்", ar: "اسحب الملفات وأفلتها هنا، أو انقر للتصفح" },
  "media.uploadTypes": { en: "Images (PNG/JPEG/WebP/GIF), code files (CSS/JS/HTML/SVG/WOFF2) and PDFs. Up to 20MB.", ms: "Gambar (PNG/JPEG/WebP/GIF), fail kod (CSS/JS/HTML/SVG/WOFF2) dan PDF. Sehingga 20MB.", zh: "图片（PNG/JPEG/WebP/GIF）、代码文件（CSS/JS/HTML/SVG/WOFF2）和 PDF。最大 20MB。", ta: "படங்கள் (PNG/JPEG/WebP/GIF), குறியீடு கோப்புகள் (CSS/JS/HTML/SVG/WOFF2) மற்றும் PDF. 20MB வரை.", ar: "الصور (PNG/JPEG/WebP/GIF) وملفات الأكواد (CSS/JS/HTML/SVG/WOFF2) وPDF. حتى 20MB." },
  "media.imageStorage": { en: "Images are saved as JPEG. A PNG with a transparent background is kept as PNG so it stays see-through; an animated GIF keeps only its first frame.", ms: "Gambar disimpan sebagai JPEG — PNG berlatar lutsinar kekal sebagai PNG supaya latar lutsinar itu tidak hilang; GIF animasi hanya kekalkan bingkai pertama.", zh: "图片会保存为 JPEG — 带透明背景的 PNG 会保留为 PNG，透明度不会丢失；动画 GIF 只保留第一帧。", ta: "படங்கள் JPEG ஆகச் சேமிக்கப்படும் — ஒளிபுகும் பின்னணி கொண்ட PNG ஆகவே சேமிக்கப்படும், அதனால் அதன் ஒளிபுகும் தன்மை இழக்கப்படாது; அனிமேஷன் GIF முதல் சட்டகத்தை மட்டும் வைத்திருக்கும்.", ar: "تُحفظ الصور كـ JPEG — ويبقى PNG ذو الخلفية الشفافة محفوظًا بصيغة PNG حتى لا تُفقد الشفافية؛ ويحتفظ GIF المتحرك بالإطار الأول فقط." },
  "media.uploadedGifStill": { en: "Uploaded ✓. The animated GIF is saved as a still image (first frame only).", ms: "Dimuat naik ✓ — GIF animasi disimpan sebagai imej pegun (bingkai pertama sahaja).", zh: "已上传 ✓ — 动画 GIF 已保存为静态图片（仅第一帧）。", ta: "பதிவேற்றப்பட்டது ✓ — அனிமேஷன் GIF நிலையான படமாகச் சேமிக்கப்பட்டது (முதல் சட்டகம் மட்டும்).", ar: "تم الرفع ✓ — حُفظ GIF المتحرك كصورة ثابتة (الإطار الأول فقط)." },
  "media.thumbnail": { en: "Thumbnail", ms: "Thumbnail", zh: "缩略图", ta: "சிறு படம்", ar: "مصغّرة" },
  "media.medium": { en: "Medium", ms: "Sederhana", zh: "中等", ta: "நடுத்தரம்", ar: "متوسط" },
  "media.full": { en: "Full", ms: "Penuh", zh: "原图", ta: "முழு", ar: "كامل" },
  "media.sizes": { en: "Sizes", ms: "Saiz", zh: "尺寸", ta: "அளவுகள்", ar: "الأحجام" },
  "media.pdf": { en: "PDF", ms: "PDF", zh: "PDF", ta: "PDF", ar: "PDF" },
  "media.uploadTooLarge": { en: "File exceeds the 20MB limit.", ms: "Fail melebihi had 20MB.", zh: "文件超过 20MB 限制。", ta: "கோப்பு 20MB வரம்பை மீறுகிறது.", ar: "الملف يتجاوز حد 20MB." },
  "media.uploadType": { en: "File type not supported (images, code files and PDFs only).", ms: "Jenis fail tidak disokong (gambar, fail kod dan PDF sahaja).", zh: "不支持的文件类型（仅限图片、代码文件和 PDF）。", ta: "கோப்பு வகை ஆதரிக்கப்படவில்லை (படங்கள், குறியீடு கோப்புகள் மற்றும் PDF மட்டும்).", ar: "نوع الملف غير مدعوم (الصور وملفات الأكواد وPDF فقط)." },
  "media.uploadEmpty": { en: "No file selected.", ms: "Tiada fail dipilih.", zh: "未选择文件。", ta: "கோப்பு தேர்ந்தெடுக்கப்படவில்லை.", ar: "لم يتم تحديد ملف." },
  // issue #384: per-workspace storage quota
  "media.usageLabel": { en: "Storage used", ms: "Storan digunakan", zh: "已用存储", ta: "பயன்படுத்திய சேமிப்பு", ar: "التخزين المستخدم" },
  // issue #387: super-admin platform-wide dashboard (/admin)
  "nav.admin": { en: "Admin", ms: "Admin", zh: "管理", ta: "நிர்வாகம்", ar: "الإدارة" },
  "admin.title": { en: "Platform overview", ms: "Gambaran keseluruhan platform", zh: "平台概览", ta: "தள கண்ணோட்டம்", ar: "نظرة عامة على المنصة" },
  "admin.subtitle": { en: "Usage across all workspaces (read-only)", ms: "Penggunaan merentas semua ruang kerja (baca sahaja)", zh: "所有工作区的使用情况（只读）", ta: "அனைத்து பணியிடங்களிலும் பயன்பாடு (படிக்க மட்டும்)", ar: "الاستخدام عبر جميع مساحات العمل (للقراءة فقط)" },
  "admin.totals": { en: "Platform totals", ms: "Jumlah platform", zh: "平台总计", ta: "தள மொத்தங்கள்", ar: "إجماليات المنصة" },
  "admin.workspaces": { en: "Workspaces", ms: "Ruang Kerja", zh: "工作区", ta: "பணியிடங்கள்", ar: "مساحات العمل" },
  "admin.forms": { en: "Forms", ms: "Borang", zh: "表单", ta: "படிவங்கள்", ar: "النماذج" },
  "admin.products": { en: "Products", ms: "Produk", zh: "产品", ta: "தயாரிப்புகள்", ar: "المنتجات" },
  "admin.sales": { en: "Sales", ms: "Jualan", zh: "销售", ta: "விற்பனைகள்", ar: "المبيعات" },
  "admin.users": { en: "Users", ms: "Pengguna", zh: "用户", ta: "பயனர்கள்", ar: "المستخدمون" },
  "admin.growth24h": { en: "New in 24h", ms: "Baharu dalam 24j", zh: "24 小时内新增", ta: "24 மணிநேரத்தில் புதியவை", ar: "جديد خلال 24 ساعة" },
  "admin.growth7d": { en: "New in 7d", ms: "Baharu dalam 7h", zh: "7 天内新增", ta: "7 நாட்களில் புதியவை", ar: "جديد خلال 7 أيام" },
  "admin.email": { en: "Email delivery", ms: "Penghantaran emel", zh: "邮件投递", ta: "மின்னஞ்சல் விநியோகம்", ar: "تسليم البريد" },
  "admin.emailDelivered": { en: "Delivered", ms: "Dihantar", zh: "已投递", ta: "வழங்கப்பட்டது", ar: "تم التسليم" },
  "admin.emailBounce": { en: "Bounces", ms: "Bounce", zh: "退信", ta: "திருப்பி", ar: "ارتدادات" },
  "admin.emailComplaint": { en: "Complaints", ms: "Aduan", zh: "投诉", ta: "புகார்கள்", ar: "شكاوى" },
  "admin.emailOpen": { en: "Opens", ms: "Bukaan", zh: "打开", ta: "திறப்புகள்", ar: "فتحات" },
  "admin.emailClick": { en: "Clicks", ms: "Klik", zh: "点击", ta: "கிளிக்குகள்", ar: "نقرات" },
  "admin.spam": { en: "Spam queue", ms: "Baris gilir spam", zh: "垃圾队列", ta: "ஸ்பேம் வரிசை", ar: "قائمة البريد العشوائي" },
  "admin.deadLetters": { en: "Callback dead letters", ms: "Dead letter callback", zh: "回调死信", ta: "அழைப்பு இறந்த கடிதங்கள்", ar: "رسائل الاستدعاء الميتة" },
  "admin.deadLettersLoadFail": { en: "Could not load dead letters.", ms: "Gagal memuatkan dead letter.", zh: "无法加载死信。", ta: "இறந்த கடிதங்களை ஏற்ற முடியவில்லை.", ar: "تعذر تحميل الرسائل الميتة." },
  "admin.deadLettersHint": { en: "Inbound payment callbacks parked because they could not be processed. They are no longer retried; requeue one after fixing the cause.", ms: "Callback pembayaran masuk yang di-park kerana tidak dapat diproses. Ia tidak lagi dicuba; hidupkan semula selepas punca dibetulkan.", zh: "因无法处理而搁置的入站支付回调。不再重试；修复原因后可重新入队。", ta: "செயலாக்க முடியாத உள்வரும் கட்டண அழைப்புகள். மீண்டும் முயற்சிக்கப்படாது; காரணத்தை சரிசெய்த பின் மீள்வரிசைப்படுத்தவும்.", ar: "عمليات استدعاء الدفع الواردة التي تم إيقافها لتعذر معالجتها. لم تعد تُعاد المحاولة؛ أعد إدراجها بعد إصلاح السبب." },
  "admin.deadLettersNone": { en: "No dead letters.", ms: "Tiada dead letter.", zh: "没有死信。", ta: "இறந்த கடிதங்கள் இல்லை.", ar: "لا توجد رسائل ميتة." },
  "admin.dlEvent": { en: "Event", ms: "Peristiwa", zh: "事件", ta: "நிகழ்வு", ar: "الحدث" },
  "admin.dlWorkspace": { en: "Workspace", ms: "Ruang kerja", zh: "工作区", ta: "பணியிடம்", ar: "مساحة العمل" },
  "admin.dlAttempts": { en: "Attempts", ms: "Cubaan", zh: "尝试次数", ta: "முயற்சிகள்", ar: "المحاولات" },
  "admin.dlReason": { en: "Reason", ms: "Sebab", zh: "原因", ta: "காரணம்", ar: "السبب" },
  "admin.dlParked": { en: "Parked since", ms: "Di-park sejak", zh: "搁置时间", ta: "நிறுத்தப்பட்டது", ar: "موقوفة منذ" },
  "admin.dlAction": { en: "Action", ms: "Tindakan", zh: "操作", ta: "செயல்", ar: "إجراء" },
  "admin.dlRequeue": { en: "Requeue", ms: "Hidupkan semula", zh: "重新入队", ta: "மீள்வரிசை", ar: "إعادة إدراج" },
  "admin.dlRequeueing": { en: "Requeuing…", ms: "Menghidupkan…", zh: "正在入队…", ta: "மீள்வரிசைப்படுத்துகிறது…", ar: "جارٍ الإدراج…" },
  "admin.dlUnattributed": { en: "unattributed", ms: "tanpa pemilik", zh: "未归属", ta: "பொறுப்பற்றது", ar: "غير منسوب" },
  // issue #601: suppression list — the addresses JomForm refuses to mail.
  "admin.supp": { en: "Email suppression list", ms: "Senarai sekatan emel", zh: "邮件抑制列表", ta: "மின்னஞ்சல் தடுப்புப் பட்டியல்", ar: "قائمة حظر البريد" },
  "admin.suppHint": { en: "Addresses JomForm will not send to. Hard bounces and spam complaints are added automatically; add or remove one here to correct a false positive.", ms: "Alamat yang JomForm tidak akan hantar. Hard bounce dan aduan spam ditambah automatik; tambah atau buang di sini untuk membetulkan positif palsu.", zh: "JomForm 不会发送到的地址。硬退信和垃圾投诉会自动加入；可在此添加或移除以纠正误判。", ta: "JomForm அனுப்பாத முகவரிகள். ஹார்ட் பவுன்ஸ் மற்றும் ஸ்பேம் புகார்கள் தானாக சேர்க்கப்படும்; தவறான கண்டறிதலைத் திருத்த இங்கே சேர் அல்லது நீக்கு.", ar: "العناوين التي لن يرسل إليها JomForm. تُضاف الارتدادات الصلبة وشكاوى البريد العشوائي تلقائياً؛ أضف أو أزل عنواناً هنا لتصحيح نتيجة خاطئة." },
  "admin.suppDocs": { en: "How the suppression list works (SES reputation)", ms: "Cara senarai sekatan berfungsi (reputasi SES)", zh: "抑制列表如何运作（SES 声誉）", ta: "தடுப்புப் பட்டியல் எவ்வாறு செயல்படுகிறது (SES நற்பெயர்)", ar: "كيف تعمل قائمة الحظر (سمعة SES)" },
  "admin.suppSearchPh": { en: "Search email or note", ms: "Cari emel atau nota", zh: "搜索邮箱或备注", ta: "மின்னஞ்சல் அல்லது குறிப்பைத் தேடு", ar: "ابحث في البريد أو الملاحظة" },
  "admin.suppLoadFail": { en: "Could not load the suppression list.", ms: "Gagal memuatkan senarai sekatan.", zh: "无法加载抑制列表。", ta: "தடுப்புப் பட்டியலை ஏற்ற முடியவில்லை.", ar: "تعذر تحميل قائمة الحظر." },
  "admin.suppNone": { en: "No suppressed addresses.", ms: "Tiada alamat disekat.", zh: "没有已抑制的地址。", ta: "தடுக்கப்பட்ட முகவரிகள் இல்லை.", ar: "لا توجد عناوين محظورة." },
  "admin.suppNoMatch": { en: "No address matches that search.", ms: "Tiada alamat sepadan carian itu.", zh: "没有地址匹配该搜索。", ta: "அந்தத் தேடலுக்கு முகவரி இல்லை.", ar: "لا يوجد عنوان يطابق هذا البحث." },
  "admin.suppColEmail": { en: "Email", ms: "Emel", zh: "邮箱", ta: "மின்னஞ்சல்", ar: "البريد" },
  "admin.suppColReason": { en: "Reason", ms: "Sebab", zh: "原因", ta: "காரணம்", ar: "السبب" },
  "admin.suppColSource": { en: "Source", ms: "Sumber", zh: "来源", ta: "மூலம்", ar: "المصدر" },
  "admin.suppColNote": { en: "Note", ms: "Nota", zh: "备注", ta: "குறிப்பு", ar: "ملاحظة" },
  "admin.suppColAdded": { en: "Added", ms: "Ditambah", zh: "添加时间", ta: "சேர்க்கப்பட்டது", ar: "أُضيف" },
  "admin.suppColBy": { en: "Added by", ms: "Ditambah oleh", zh: "添加者", ta: "சேர்த்தவர்", ar: "أضافه" },
  "admin.suppColAction": { en: "Action", ms: "Tindakan", zh: "操作", ta: "செயல்", ar: "إجراء" },
  "admin.suppRemove": { en: "Remove", ms: "Buang", zh: "移除", ta: "நீக்கு", ar: "إزالة" },
  "admin.suppRemoving": { en: "Removing…", ms: "Membuang…", zh: "正在移除…", ta: "நீக்குகிறது…", ar: "جارٍ الإزالة…" },
  "admin.suppRemoveConfirm": { en: "Send email to {email} again?", ms: "Hantar emel ke {email} semula?", zh: "重新向 {email} 发送邮件？", ta: "{email} க்கு மீண்டும் மின்னஞ்சல் அனுப்பவா?", ar: "إرسال البريد إلى {email} مرة أخرى؟" },
  "admin.suppRemoveFail": { en: "Could not remove that address.", ms: "Gagal membuang alamat itu.", zh: "无法移除该地址。", ta: "அந்த முகவரியை நீக்க முடியவில்லை.", ar: "تعذر إزالة هذا العنوان." },
  "admin.suppAddTitle": { en: "Add an address", ms: "Tambah alamat", zh: "添加地址", ta: "முகவரியைச் சேர்", ar: "إضافة عنوان" },
  "admin.suppAddEmail": { en: "Email", ms: "Emel", zh: "邮箱", ta: "மின்னஞ்சல்", ar: "البريد" },
  "admin.suppAddReason": { en: "Reason", ms: "Sebab", zh: "原因", ta: "காரணம்", ar: "السبب" },
  "admin.suppAddNote": { en: "Note (optional)", ms: "Nota (pilihan)", zh: "备注（可选）", ta: "குறிப்பு (விருப்பம்)", ar: "ملاحظة (اختياري)" },
  "admin.suppAddSubmit": { en: "Suppress", ms: "Sekat", zh: "抑制", ta: "தடு", ar: "حظر" },
  "admin.suppAdding": { en: "Adding…", ms: "Menambah…", zh: "正在添加…", ta: "சேர்க்கிறது…", ar: "جارٍ الإضافة…" },
  "admin.suppAdded": { en: "{email} will no longer receive email.", ms: "{email} tidak akan menerima emel lagi.", zh: "{email} 将不再收到邮件。", ta: "{email} இனி மின்னஞ்சல் பெறாது.", ar: "لن يتلقى {email} أي بريد بعد الآن." },
  "admin.suppAddedExisting": { en: "{email} was already on the list.", ms: "{email} sudah ada dalam senarai.", zh: "{email} 已在列表中。", ta: "{email} ஏற்கனவே பட்டியலில் உள்ளது.", ar: "{email} موجود بالفعل في القائمة." },
  "admin.suppBadEmail": { en: "Enter a valid email address.", ms: "Masukkan alamat emel yang sah.", zh: "请输入有效的邮箱地址。", ta: "சரியான மின்னஞ்சல் முகவரியை உள்ளிடவும்.", ar: "أدخل عنوان بريد إلكتروني صالح." },
  "admin.suppAddFail": { en: "Could not add that address.", ms: "Gagal menambah alamat itu.", zh: "无法添加该地址。", ta: "அந்த முகவரியைச் சேர்க்க முடியவில்லை.", ar: "تعذر إضافة هذا العنوان." },
  "admin.suppCount": { en: "{n} suppressed", ms: "{n} disekat", zh: "已抑制 {n} 个", ta: "{n} தடுக்கப்பட்டது", ar: "{n} محظور" },
  "admin.suppPageOf": { en: "page {page} of {total}", ms: "halaman {page} daripada {total}", zh: "第 {page} 页，共 {total} 页", ta: "பக்கம் {page} / {total}", ar: "صفحة {page} من {total}" },
  "admin.spamSubmissions": { en: "Submissions", ms: "Penghantaran", zh: "提交", ta: "சமர்ப்பிப்புகள்", ar: "الطلبات" },
  "admin.spamSales": { en: "Sales", ms: "Jualan", zh: "销售", ta: "விற்பனைகள்", ar: "المبيعات" },
  "admin.storage": { en: "Storage per workspace", ms: "Storan setiap ruang kerja", zh: "每个工作区的存储", ta: "ஒவ்வொரு பணியிடத்திற்கும் சேமிப்பு", ar: "التخزين لكل مساحة عمل" },
  "admin.storageUsed": { en: "Used", ms: "Digunakan", zh: "已用", ta: "பயன்படுத்தியது", ar: "المستخدم" },
  "admin.quota": { en: "Quota", ms: "Kuota", zh: "配额", ta: "ஒதுக்கீடு", ar: "الحصة" },
  "admin.recentSignups": { en: "Recent signups", ms: "Pendaftaran terkini", zh: "最近注册", ta: "சமீபத்திய பதிவுகள்", ar: "عمليات التسجيل الأخيرة" },
  "admin.activeWorkspaces": { en: "Active workspaces (7d)", ms: "Ruang kerja aktif (7h)", zh: "活跃工作区（7 天）", ta: "செயலில் உள்ள பணியிடங்கள் (7 நாட்கள்)", ar: "مساحات العمل النشطة (7 أيام)" },
  "admin.lastActivity": { en: "Last activity", ms: "Aktiviti terakhir", zh: "最近活动", ta: "கடைசி செயல்பாடு", ar: "آخر نشاط" },
  "admin.owner": { en: "Owner", ms: "Pemilik", zh: "所有者", ta: "உரிமையாளர்", ar: "المالك" },
  "admin.readonly": { en: "Read-only view", ms: "Paparan baca sahaja", zh: "只读视图", ta: "படிக்க மட்டும் காட்சி", ar: "عرض للقراءة فقط" },
  "admin.forbidden": { en: "You don't have permission to view the admin dashboard.", ms: "Anda tiada kebenaran untuk melihat papan pemuka admin.", zh: "您没有权限查看管理仪表板。", ta: "நிர்வாக டாஷ்போர்டைப் பார்க்க உங்களுக்கு அனுமதி இல்லை.", ar: "ليس لديك إذن لعرض لوحة تحكم الإدارة." },
  "admin.loadFail": { en: "We couldn't load the overview. Please try again.", ms: "Kami tidak dapat memuatkan ringkasan. Sila cuba lagi.", zh: "无法加载概览。请重试。", ta: "மேலோட்டத்தை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل النظرة العامة. يرجى المحاولة مرة أخرى." },

  // ── Dormancy report (card t_94e5cca3, phase 1) ────────────────────────────
  // Phase 1 of the three-phase dormancy policy: a READ-ONLY per-account report plus
  // one reminder email. The copy below must never imply that anything is deleted or
  // unpublished — phases 2 and 3 are separate decisions that have not been built.
  "admin.dormTitle": { en: "Dormancy report", ms: "Laporan dormansi", zh: "休眠报告", ta: "செயலின்மை அறிக்கை", ar: "تقرير الخمول" },
  "admin.dormHint": { en: "Which workspaces have gone quiet, and whether each one may be treated as dormant. Read-only: nothing here deletes, unpublishes or flags an account.", ms: "Ruang kerja mana yang sudah senyap, dan sama ada setiap satunya boleh dianggap dorman. Baca sahaja: tiada apa di sini memadam, menyahterbit atau menanda akaun.", zh: "哪些工作区已经没有动静，以及每个是否可以视为休眠。只读：此处不会删除、取消发布或标记任何账户。", ta: "எந்த பணியிடங்கள் அமைதியாகிவிட்டன, ஒவ்வொன்றையும் செயலற்றதாகக் கருதலாமா என்பதைக் காட்டுகிறது. படிக்க மட்டும்: இங்கு எதுவும் நீக்கப்படவோ வெளியீடு நீக்கப்படவோ குறிக்கப்படவோ இல்லை.", ar: "أي مساحات العمل أصبحت هادئة، وهل يمكن اعتبار كل منها خامدة. للقراءة فقط: لا شيء هنا يحذف أو يلغي نشر أو يضع علامة على أي حساب." },
  "admin.dormDocs": { en: "Dormant workspaces and the retention policy", ms: "Ruang kerja dorman dan dasar penyimpanan", zh: "休眠工作区与保留政策", ta: "செயலற்ற பணியிடங்கள் மற்றும் தக்கவைப்புக் கொள்கை", ar: "مساحات العمل الخامدة وسياسة الاحتفاظ" },
  "admin.dormCandidates": { en: "Candidates: {n}", ms: "Calon: {n}", zh: "候选：{n}", ta: "வேட்பாளர்கள்: {n}", ar: "المرشحون: {n}" },
  "admin.dormExcluded": { en: "Excluded: {n}", ms: "Dikecualikan: {n}", zh: "已排除：{n}", ta: "விலக்கப்பட்டவை: {n}", ar: "المستبعدون: {n}" },
  "admin.dormNudgeable": { en: "Reminders due: {n}", ms: "Peringatan perlu dihantar: {n}", zh: "待发提醒：{n}", ta: "அனுப்ப வேண்டிய நினைவூட்டல்கள்: {n}", ar: "التذكيرات المستحقة: {n}" },
  "admin.dormTruncated": { en: "Showing the {n} most dormant accounts. The report is capped.", ms: "Menunjukkan {n} akaun paling dorman. Laporan ini dihadkan.", zh: "仅显示最休眠的 {n} 个账户。报告有上限。", ta: "மிகவும் செயலற்ற {n} கணக்குகள் காட்டப்படுகின்றன. அறிக்கை வரம்பிடப்பட்டுள்ளது.", ar: "يتم عرض أكثر {n} حساباً خمولاً. التقرير محدود." },
  "admin.dormNone": { en: "No accounts to report.", ms: "Tiada akaun untuk dilaporkan.", zh: "没有需要报告的账户。", ta: "அறிக்கையிட கணக்குகள் இல்லை.", ar: "لا توجد حسابات للإبلاغ عنها." },
  "admin.dormLoadFail": { en: "We couldn't load the dormancy report. Please try again.", ms: "Kami tidak dapat memuatkan laporan dormansi. Sila cuba lagi.", zh: "无法加载休眠报告。请重试。", ta: "செயலின்மை அறிக்கையை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل تقرير الخمول. يرجى المحاولة مرة أخرى." },
  "admin.dormBucket": { en: "Bucket", ms: "Kumpulan", zh: "分组", ta: "குழு", ar: "الفئة" },
  "admin.dormBucket.active": { en: "Active", ms: "Aktif", zh: "活跃", ta: "செயலில்", ar: "نشط" },
  "admin.dormBucket.30": { en: "30 days", ms: "30 hari", zh: "30 天", ta: "30 நாட்கள்", ar: "30 يوماً" },
  "admin.dormBucket.60": { en: "60 days", ms: "60 hari", zh: "60 天", ta: "60 நாட்கள்", ar: "60 يوماً" },
  "admin.dormBucket.90": { en: "90 days", ms: "90 hari", zh: "90 天", ta: "90 நாட்கள்", ar: "90 يوماً" },
  "admin.dormBucket.365": { en: "365 days", ms: "365 hari", zh: "365 天", ta: "365 நாட்கள்", ar: "365 يوماً" },
  "admin.dormBucket.excluded": { en: "Excluded", ms: "Dikecualikan", zh: "已排除", ta: "விலக்கப்பட்டது", ar: "مستبعد" },
  "admin.dormAccount": { en: "Account", ms: "Akaun", zh: "账户", ta: "கணக்கு", ar: "الحساب" },
  "admin.dormCreated": { en: "Created", ms: "Dicipta", zh: "创建时间", ta: "உருவாக்கப்பட்டது", ar: "تاريخ الإنشاء" },
  "admin.dormDays": { en: "{n} days quiet", ms: "{n} hari senyap", zh: "已 {n} 天无活动", ta: "{n} நாட்கள் அமைதி", ar: "{n} يوماً من الهدوء" },
  "admin.dormFormsPublished": { en: "Forms online", ms: "Borang dalam talian", zh: "在线表单", ta: "இணையத்தில் உள்ள படிவங்கள்", ar: "النماذج المنشورة" },
  "admin.dormSubmissions": { en: "Responses (90d)", ms: "Respons (90h)", zh: "回复（90 天）", ta: "பதில்கள் (90 நாட்கள்)", ar: "الردود (90 يوماً)" },
  "admin.dormReason": { en: "Excluded because", ms: "Dikecualikan kerana", zh: "排除原因", ta: "விலக்கப்பட்டதற்கான காரணம்", ar: "سبب الاستبعاد" },
  "admin.dormNoReason": { en: "None", ms: "Tiada", zh: "无", ta: "இல்லை", ar: "لا يوجد" },
  "admin.dormSales": { en: "Sales (ever)", ms: "Jualan (keseluruhan)", zh: "销售（累计）", ta: "விற்பனைகள் (மொத்தம்)", ar: "المبيعات (الإجمالي)" },

  // ── In-app feedback (user surface + admin triage) ─────────────────────────
  // The nav link is deliberately NOT admin-gated: every signed-in user can
  // reach the feedback page, so these strings must read naturally to a merchant
  // who has never seen the admin side.
  "nav.feedback": { en: "Feedback", ms: "Maklum Balas", zh: "反馈", ta: "கருத்து", ar: "الملاحظات" },
  "fb.title": { en: "Feedback & feature requests", ms: "Maklum balas & permintaan ciri", zh: "反馈与功能请求", ta: "கருத்து & அம்ச கோரிக்கைகள்", ar: "الملاحظات وطلبات الميزات" },
  "fb.subtitle": { en: "Tell us what is broken or what you need. We reply in the thread below and you can come back to read it any time.", ms: "Beritahu kami apa yang rosak atau apa yang anda perlukan. Kami membalas dalam thread di bawah dan anda boleh kembali membacanya bila-bila masa.", zh: "告诉我们哪里出了问题或您需要什么。我们会在下方的对话中回复，您可以随时回来查看。", ta: "என்ன உடைந்துள்ளது அல்லது உங்களுக்கு என்ன தேவை என்பதைச் சொல்லுங்கள். கீழே உள்ள உரையாடலில் நாங்கள் பதிலளிப்போம், எப்போது வேண்டுமானாலும் திரும்பி வந்து படிக்கலாம்.", ar: "أخبرنا بما لا يعمل أو بما تحتاجه. نرد في المحادثة أدناه ويمكنك العودة لقراءتها في أي وقت." },
  "fb.navHint": { en: "Every signed-in user can send feedback here. It is not tied to any workspace.", ms: "Setiap pengguna yang log masuk boleh menghantar maklum balas di sini — ia tidak terikat pada mana-mana ruang kerja.", zh: "每位已登录用户都可在此发送反馈——它不隶属于任何工作区。", ta: "உள்நுழைந்த ஒவ்வொரு பயனரும் இங்கே கருத்து அனுப்பலாம் — இது எந்த பணியிடத்துடனும் இணைக்கப்படவில்லை.", ar: "يمكن لكل مستخدم مسجَّل الدخول إرسال ملاحظات هنا — وهي غير مرتبطة بأي مساحة عمل." },
  "fb.new": { en: "Send feedback", ms: "Hantar maklum balas", zh: "发送反馈", ta: "கருத்து அனுப்பு", ar: "إرسال ملاحظات" },
  "fb.subject": { en: "Subject (optional)", ms: "Tajuk (pilihan)", zh: "主题（可选）", ta: "தலைப்பு (விருப்பம்)", ar: "الموضوع (اختياري)" },
  "fb.subjectPh": { en: "One line describing it", ms: "Satu baris menerangkannya", zh: "用一句话描述", ta: "ஒரு வரியில் விவரிக்கவும்", ar: "سطر واحد يصفه" },
  "fb.body": { en: "Your feedback", ms: "Maklum balas anda", zh: "您的反馈", ta: "உங்கள் கருத்து", ar: "ملاحظاتك" },
  "fb.bodyPh": { en: "What happened, or what would help? Minimum 10 characters.", ms: "Apa yang berlaku, atau apa yang membantu? Minimum 10 aksara.", zh: "发生了什么，或什么会有帮助？最少 10 个字符。", ta: "என்ன நடந்தது அல்லது எது உதவும்? குறைந்தது 10 எழுத்துகள்.", ar: "ماذا حدث، أو ما الذي سيساعد؟ 10 أحرف على الأقل." },
  "fb.submit": { en: "Send feedback", ms: "Hantar maklum balas", zh: "发送反馈", ta: "கருத்து அனுப்பு", ar: "إرسال الملاحظات" },
  "fb.sending": { en: "Sending…", ms: "Menghantar…", zh: "正在发送…", ta: "அனுப்புகிறது…", ar: "جارٍ الإرسال…" },
  "fb.sent": { en: "Thank you. We received your feedback.", ms: "Terima kasih — kami telah menerima maklum balas anda.", zh: "谢谢——我们已收到您的反馈。", ta: "நன்றி — உங்கள் கருத்தைப் பெற்றுக்கொண்டோம்.", ar: "شكرًا لك — لقد تلقّينا ملاحظاتك." },
  "fb.sendFail": { en: "We couldn't send that. Please try again.", ms: "Kami tidak dapat menghantarnya. Sila cuba lagi.", zh: "无法发送。请重试。", ta: "அதை அனுப்ப முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر الإرسال. يرجى المحاولة مرة أخرى." },
  "fb.tooShort": { en: "Please write at least 10 characters.", ms: "Sila tulis sekurang-kurangnya 10 aksara.", zh: "请至少填写 10 个字符。", ta: "குறைந்தது 10 எழுத்துகள் எழுதவும்.", ar: "يرجى كتابة 10 أحرف على الأقل." },
  "fb.rateLimited": { en: "You have sent the maximum number of feedback items for today (10). Please continue in an existing thread, or try again tomorrow.", ms: "Anda telah menghantar bilangan maksimum maklum balas untuk hari ini (10). Sila sambung dalam thread sedia ada, atau cuba lagi esok.", zh: "您今天发送的反馈已达上限（10 条）。请在现有对话中继续，或明天再试。", ta: "இன்று அதிகபட்ச கருத்துகளை அனுப்பிவிட்டீர்கள் (10). ஏற்கனவே உள்ள உரையாடலில் தொடரவும் அல்லது நாளை மீண்டும் முயற்சிக்கவும்.", ar: "لقد أرسلت الحد الأقصى من الملاحظات اليوم (10). تابع في محادثة موجودة، أو حاول غدًا." },
  "fb.mine": { en: "Your threads", ms: "Thread anda", zh: "您的对话", ta: "உங்கள் உரையாடல்கள்", ar: "محادثاتك" },
  "fb.empty": { en: "You have not sent any feedback yet.", ms: "Anda belum menghantar sebarang maklum balas.", zh: "您尚未发送任何反馈。", ta: "நீங்கள் இன்னும் கருத்து அனுப்பவில்லை.", ar: "لم ترسل أي ملاحظات بعد." },
  "fb.pickOne": { en: "Select a thread to read the conversation.", ms: "Pilih satu thread untuk membaca perbualan.", zh: "选择一个对话以阅读内容。", ta: "உரையாடலைப் படிக்க ஒரு உரையாடலைத் தேர்ந்தெடுக்கவும்.", ar: "اختر محادثة لقراءة المحتوى." },
  "fb.noSubject": { en: "(no subject)", ms: "(tanpa tajuk)", zh: "（无主题）", ta: "(தலைப்பு இல்லை)", ar: "(بدون موضوع)" },
  "fb.messages": { en: "{n} messages", ms: "{n} mesej", zh: "{n} 条消息", ta: "{n} செய்திகள்", ar: "{n} رسائل" },
  "fb.reply": { en: "Add a message", ms: "Tambah mesej", zh: "添加消息", ta: "செய்தி சேர்", ar: "إضافة رسالة" },
  "fb.replyPh": { en: "Add more detail…", ms: "Tambah butiran lain…", zh: "补充更多细节…", ta: "மேலும் விவரம் சேர்…", ar: "أضف مزيدًا من التفاصيل…" },
  "fb.replySend": { en: "Send", ms: "Hantar", zh: "发送", ta: "அனுப்பு", ar: "إرسال" },
  "fb.replySent": { en: "Message added.", ms: "Mesej ditambah.", zh: "消息已添加。", ta: "செய்தி சேர்க்கப்பட்டது.", ar: "تمت إضافة الرسالة." },
  "fb.whoUser": { en: "You", ms: "Anda", zh: "您", ta: "நீங்கள்", ar: "أنت" },
  "fb.whoTeam": { en: "JomForm team", ms: "Pasukan JomForm", zh: "JomForm 团队", ta: "JomForm குழு", ar: "فريق JomForm" },
  "fb.status.open": { en: "Open", ms: "Terbuka", zh: "待处理", ta: "திறந்தது", ar: "مفتوح" },
  "fb.status.in_progress": { en: "In progress", ms: "Sedang diproses", zh: "处理中", ta: "செயல்பாட்டில்", ar: "قيد المعالجة" },
  "fb.status.resolved": { en: "Resolved", ms: "Selesai", zh: "已解决", ta: "தீர்க்கப்பட்டது", ar: "تم الحل" },
  "fb.status.closed": { en: "Closed", ms: "Ditutup", zh: "已关闭", ta: "மூடப்பட்டது", ar: "مغلق" },
  "fb.loadFail": { en: "We couldn't load your feedback. Please try again.", ms: "Kami tidak dapat memuatkan maklum balas anda. Sila cuba lagi.", zh: "无法加载您的反馈。请重试。", ta: "உங்கள் கருத்துகளை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل ملاحظاتك. يرجى المحاولة مرة أخرى." },
  "fb.docs": { en: "How feedback works", ms: "Cara maklum balas berfungsi", zh: "反馈如何运作", ta: "கருத்து எவ்வாறு செயல்படுகிறது", ar: "كيف تعمل الملاحظات" },
  "fb.adminHint": { en: "You are a platform admin. The Admin tab shows every thread for triage.", ms: "Anda pentadbir platform — tab Admin menunjukkan setiap thread untuk triaj.", zh: "您是平台管理员——管理标签页显示所有对话以便分类处理。", ta: "நீங்கள் தள நிர்வாகி — நிர்வாகி தாவல் ஒவ்வொரு உரையாடலையும் காட்டுகிறது.", ar: "أنت مسؤول المنصة — تبويب الإدارة يعرض كل المحادثات للفرز." },

  // ── In-app assistant (phase 1) ───────────────────────────────────────────
  // The chat is READ-ONLY apart from filing feedback. The strings say so
  // plainly: "it cannot change your account" is the feature, not a disclaimer,
  // and a user who asks it to delete a form must read why it will not.
  "nav.chat": { en: "Assistant", ms: "Pembantu", zh: "助手", ta: "உதவியாளர்", ar: "المساعد" },
  "chat.title": { en: "Assistant", ms: "Pembantu", zh: "助手", ta: "உதவியாளர்", ar: "المساعد" },
  "chat.subtitle": { en: "Ask about your own forms, sales and products. It can file feedback, and it can prepare a change for you to confirm. It never changes your account by itself.", ms: "Tanya tentang borang, jualan dan produk anda sendiri. Ia boleh menghantar maklum balas dan menyediakan perubahan untuk anda sahkan — ia tidak pernah mengubah akaun anda sendiri.", zh: "询问您自己的表单、销售和产品。它可以提交反馈，也可以准备变更供您确认——它绝不会自行更改您的账户。", ta: "உங்கள் சொந்த படிவங்கள், விற்பனை மற்றும் தயாரிப்புகள் பற்றி கேளுங்கள். கருத்தை அனுப்பலாம், மேலும் மாற்றத்தை உங்கள் உறுதிப்படுத்தலுக்குத் தயார் செய்யலாம் — உங்கள் கணக்கை அது தானாக மாற்றாது.", ar: "اسأل عن نماذجك ومبيعاتك ومنتجاتك. يمكنه إرسال ملاحظات، ويمكنه تحضير تغيير لتؤكده — لكنه لا يغيّر حسابك من تلقاء نفسه." },
  "chat.readOnlyBadge": { en: "Confirms with you", ms: "Sahkan dengan anda", zh: "由您确认", ta: "நீங்கள் உறுதிப்படுத்துவீர்கள்", ar: "بتأكيدك" },
  // SHORT ENOUGH TO FIT THE COMPOSER ON A PHONE (card t_804040d3). Measured at
  // 390px with the composer's own font (14px Inter, 210px of content box): the
  // old English string ("Ask a question about your data…") is 217.59px and the
  // Malay one 222.51px, so both were clipped mid-word in the one-row textarea.
  // These two measure 144.81px and 175.27px. zh/ta/ar already fit (they are
  // 98–188px) and are unchanged; e2e/a11y-chat-composer.spec.ts measures every
  // locale rather than trusting this note.
  "chat.placeholder": { en: "Ask about your data…", ms: "Tanya tentang data anda…", zh: "就您的数据提问…", ta: "உங்கள் தரவு பற்றி கேளுங்கள்…", ar: "اطرح سؤالاً عن بياناتك…" },
  "chat.send": { en: "Send", ms: "Hantar", zh: "发送", ta: "அனுப்பு", ar: "إرسال" },
  "chat.thinking": { en: "Looking that up…", ms: "Sedang mencari…", zh: "正在查询…", ta: "தேடுகிறது…", ar: "جارٍ البحث…" },
  // ── Streaming, Stop, the live step line and the copy affordance (t_148d7100) ─
  //
  // The status line is SEPARATE from the Send button: the button said "Looking
  // that up…" WHILE SENDING, so the one control a user presses was also the only
  // place the state was shown, and the same sentence appeared a second time in the
  // transcript. `chat.send` is now always the button's label (the e2e specs look
  // it up by name) and these keys carry the state.
  "chat.stop": { en: "Stop", ms: "Henti", zh: "停止", ta: "நிறுத்து", ar: "إيقاف" },
  "chat.stopped": { en: "Stopped. This answer is incomplete.", ms: "Dihentikan — jawapan ini tidak lengkap.", zh: "已停止——此回答不完整。", ta: "நிறுத்தப்பட்டது — இந்தப் பதில் முழுமையடையவில்லை.", ar: "تم الإيقاف — هذه الإجابة غير مكتملة." },
  "chat.copy": { en: "Copy", ms: "Salin", zh: "复制", ta: "நகலெடு", ar: "نسخ" },
  "chat.copied": { en: "Copied", ms: "Disalin", zh: "已复制", ta: "நகலெடுக்கப்பட்டது", ar: "تم النسخ" },
  "chat.jumpLatest": { en: "Jump to latest", ms: "Ke jawapan terkini", zh: "跳到最新", ta: "சமீபத்தியதற்குச் செல்", ar: "الانتقال إلى الأحدث" },
  // The step line. A tool name becomes a SENTENCE here, in the visitor's
  // language: the API sends the tool name only (issue #6177), and printing
  // `list_form_submissions` at a merchant would be the same defect as printing a
  // machine key. The noun is a separate key so the sentence's word order is the
  // translator's, not an English concatenation's.
  "chat.step.reading": { en: "Reading your {thing}…", ms: "Membaca {thing} anda…", zh: "正在读取您的{thing}…", ta: "உங்கள் {thing} படிக்கிறது…", ar: "جارٍ قراءة {thing}…" },
  "chat.step.filing": { en: "Filing that for you…", ms: "Menghantar maklum balas anda…", zh: "正在提交反馈…", ta: "உங்கள் கருத்தை அனுப்புகிறது…", ar: "جارٍ إرسال ملاحظاتك…" },
  "chat.step.working": { en: "Working on it…", ms: "Sedang diproses…", zh: "正在处理…", ta: "செயலாகிறது…", ar: "جارٍ العمل عليه…" },
  // The same three, in the past tense, for the finished step trail under an answer.
  // A trail line that still says "Reading your forms…" long after it read them
  // reads as work that never finished — the same class of lie as a card that keeps
  // saying "needs your confirmation" once it is settled.
  "chat.step.read": { en: "Read your {thing}", ms: "Baca {thing} anda", zh: "已读取您的{thing}", ta: "உங்கள் {thing} படித்தது", ar: "قرأ {thing}" },
  "chat.step.filed": { en: "Filed your feedback", ms: "Maklum balas anda dihantar", zh: "已提交您的反馈", ta: "உங்கள் கருத்தை அனுப்பியது", ar: "أرسل ملاحظاتك" },
  "chat.step.worked": { en: "Looked that up", ms: "Selesai mencari", zh: "已查询", ta: "தேடி முடிந்தது", ar: "تم البحث" },
  "chat.noun.account": { en: "account", ms: "akaun", zh: "账户", ta: "கணக்கு", ar: "الحساب" },
  "chat.noun.workspaces": { en: "workspaces", ms: "ruang kerja", zh: "工作区", ta: "பணியிடங்கள்", ar: "مساحات العمل" },
  "chat.noun.forms": { en: "forms", ms: "borang", zh: "表单", ta: "படிவங்கள்", ar: "النماذج" },
  "chat.noun.analytics": { en: "analytics", ms: "analitik", zh: "分析数据", ta: "பகுப்பாய்வு", ar: "التحليلات" },
  "chat.noun.submissions": { en: "form responses", ms: "respons borang", zh: "表单回复", ta: "படிவப் பதில்கள்", ar: "ردود النماذج" },
  "chat.noun.products": { en: "products", ms: "produk", zh: "产品", ta: "தயாரிப்புகள்", ar: "المنتجات" },
  "chat.noun.sales": { en: "sales", ms: "jualan", zh: "销售", ta: "விற்பனைகள்", ar: "المبيعات" },
  "chat.noun.subscriptions": { en: "subscriptions", ms: "langganan", zh: "订阅", ta: "சந்தாக்கள்", ar: "الاشتراكات" },
  "chat.noun.deliveries": { en: "email deliveries", ms: "penghantaran e-mel", zh: "邮件投递", ta: "மின்னஞ்சல் அனுப்புகை", ar: "تسليم البريد" },
  "chat.noun.spam": { en: "spam reports", ms: "laporan spam", zh: "垃圾邮件举报", ta: "ஸ்பேம் புகார்கள்", ar: "تقارير الرسائل المزعجة" },
  "chat.noun.media": { en: "media", ms: "media", zh: "媒体文件", ta: "மீடியா", ar: "الوسائط" },
  "chat.noun.events": { en: "activity", ms: "aktiviti", zh: "动态", ta: "செயல்பாடுகள்", ar: "النشاط" },
  "chat.noun.templates": { en: "templates", ms: "templat", zh: "模板", ta: "வார்ப்புருக்கள்", ar: "القوالب" },
  "chat.noun.feedback": { en: "feedback", ms: "maklum balas", zh: "反馈", ta: "கருத்துகள்", ar: "الملاحظات" },
  // The SETTINGS reads. The wording follows the dashboard's own labels
  // (`set.tab.fees` = Payment fees, `fs.shipSave` = shipping rates,
  // `fs.couriers` = courier providers) so the status line and the Settings page a
  // merchant is asked to open use the same words.
  "chat.noun.settings": { en: "workspace settings", ms: "tetapan ruang kerja", zh: "工作区设置", ta: "பணியிட அமைப்புகள்", ar: "إعدادات مساحة العمل" },
  "chat.noun.fees": { en: "payment fees", ms: "caj bayaran", zh: "付款手续费", ta: "கட்டணக் கட்டணங்கள்", ar: "رسوم الدفع" },
  "chat.noun.shippingRates": { en: "shipping rates", ms: "kadar penghantaran", zh: "运费", ta: "ஷிப்பிங் கட்டணங்கள்", ar: "أسعار الشحن" },
  "chat.noun.couriers": { en: "couriers", ms: "kurier", zh: "快递服务商", ta: "கூரியர்கள்", ar: "شركات الشحن" },
  // The starter chips. Each one is a QUESTION the chat's read allowlist really
  // answers, and each is SENT as the message — so it exists in every locale.
  "chat.starter.forms": { en: "Which of my forms are live?", ms: "Borang mana yang live?", zh: "我有哪些表单已上线？", ta: "எனது எந்தப் படிவங்கள் நேரலையில் உள்ளன?", ar: "أي نماذجي مُفعَّلة؟" },
  "chat.starter.sales": { en: "How many sales came in this week?", ms: "Berapa banyak jualan masuk minggu ini?", zh: "本周有多少笔销售？", ta: "இந்த வாரம் எத்தனை விற்பனைகள் வந்தன?", ar: "كم عملية بيع وردت هذا الأسبوع؟" },
  "chat.starter.submissions": { en: "Any new form responses today?", ms: "Ada respons borang baharu hari ini?", zh: "今天有新表单回复吗？", ta: "இன்று புதிய படிவப் பதில்கள் உள்ளனவா?", ar: "هل وردت ردود نماذج جديدة اليوم؟" },
  "chat.starter.help": { en: "What can you do?", ms: "Apa yang anda boleh lakukan?", zh: "你能做什么？", ta: "நீங்கள் என்ன செய்ய முடியும்?", ar: "ماذا يمكنك أن تفعل؟" },
  "chat.empty": { en: "Ask something like \"which of my forms are live?\" or \"how many sales came in this week?\".", ms: "Tanya seperti \"borang mana yang live?\" atau \"berapa banyak jualan masuk minggu ini?\".", zh: "试试问“我有哪些表单已上线？”或“本周有多少笔销售？”。", ta: "\"என்னுடைய எந்தப் படிவங்கள் நேரலையில் உள்ளன?\" அல்லது \"இந்த வாரம் எத்தனை விற்பனைகள் வந்தன?\" போன்று கேளுங்கள்.", ar: "اسأل مثلاً \"أي نماذجي مُفعَّلة؟\" أو \"كم عملية بيع وردت هذا الأسبوع؟\"." },
  "chat.fail": { en: "The assistant could not answer. Please try again.", ms: "Pembantu tidak dapat menjawab. Sila cuba lagi.", zh: "助手无法回答。请重试。", ta: "உதவியாளரால் பதிலளிக்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر على المساعد الإجابة. يرجى المحاولة مرة أخرى." },
  "chat.unavailable": { en: "The assistant is not available right now.", ms: "Pembantu tidak tersedia buat masa ini.", zh: "助手目前不可用。", ta: "உதவியாளர் இப்போது கிடைக்கவில்லை.", ar: "المساعد غير متاح حاليًا." },
  "chat.rateLimited": { en: "You have reached the chat limit (60 messages an hour). Please try again shortly.", ms: "Anda telah mencapai had chat (60 mesej sejam). Sila cuba sebentar lagi.", zh: "您已达到聊天上限（每小时 60 条）。请稍后再试。", ta: "அரட்டை வரம்பை அடைந்துவிட்டீர்கள் (மணிக்கு 60 செய்திகள்). சிறிது நேரத்தில் மீண்டும் முயற்சிக்கவும்.", ar: "لقد بلغت حد المحادثة (60 رسالة في الساعة). يرجى المحاولة بعد قليل." },
  "chat.tooLong": { en: "That message is too long. Please shorten it.", ms: "Mesej itu terlalu panjang — sila pendekkan.", zh: "该消息过长——请缩短。", ta: "அந்தச் செய்தி மிக நீளமானது — சுருக்கவும்.", ar: "هذه الرسالة طويلة جدًا — يرجى تقصيرها." },
  "chat.filed": { en: "Feedback filed. We will reply in your Feedback threads.", ms: "Maklum balas dihantar — kami akan membalas dalam thread Maklum Balas anda.", zh: "反馈已提交——我们会在您的反馈对话中回复。", ta: "கருத்து அனுப்பப்பட்டது — உங்கள் கருத்து உரையாடல்களில் நாங்கள் பதிலளிப்போம்.", ar: "تم إرسال الملاحظات — سنرد في محادثات ملاحظاتك." },
  "chat.viewThread": { en: "Open my threads", ms: "Buka thread saya", zh: "打开我的对话", ta: "எனது உரையாடல்களைத் திற", ar: "افتح محادثاتي" },
  "chat.refused": { en: "That action is not allowed here.", ms: "Tindakan itu tidak dibenarkan di sini.", zh: "此处不允许该操作。", ta: "அந்தச் செயல் இங்கே அனுமதிக்கப்படாது.", ar: "هذا الإجراء غير مسموح به هنا." },
  // ── THE BLOCK'S HEADING IS A DECISION TOO (t_7f1694cb) ────────────────────
  //
  // `chat.refused` above is the claim "this ACTION is not allowed", and it is the
  // right claim for a forbidden tool. It is the WRONG claim for a proposal whose
  // ARGUMENTS were refused: creating a form IS proposable — the chat exists to
  // prepare exactly that — so a merchant whose natural "create a form called …"
  // request was refused read a sentence telling them the feature was off-limits,
  // followed by an empty reply (measured on v1.21.149).
  //
  // The page no longer hard-codes a heading: it asks lib/chatRefusal.ts, which
  // picks this key when EVERY refused step is the proposal lane and the generic
  // one otherwise (a block that also holds a forbidden tool call really does
  // contain something not allowed). That decision is EXECUTED by
  // src/lib/chatRefusal.test.ts, and the browser half is e2e/chat-refusal-language.
  "chat.refusedHeading.proposal": { en: "That change could not be prepared.", ms: "Perubahan itu tidak dapat disediakan.", zh: "该更改未能准备。", ta: "அந்த மாற்றத்தைத் தயார் செய்ய முடியவில்லை.", ar: "لم يُتمكن من تحضير ذلك التغيير." },
  // ── ...AND SO IS "THE TOOL ITSELF REFUSED" (t_66c1f8b1) ────────────────────
  //
  // The heading above was chosen for the PROPOSAL lane, and everything else fell
  // through to `chat.refused` — including a step whose tool RAN and declined
  // (`chat.refused.tool`: `get_form` on an id that does not exist) or whose call
  // failed before it reached the tool (`chat.refused.error`). The read tool was
  // ALLOWED; the id was wrong. The merchant was told the assistant may not read
  // their forms when in fact the call was allowed and answered nothing (measured
  // live on v1.21.250, English dashboard: "That action is not allowed here. /
  // get_form — The tool refused the call, so nothing changed.").
  //
  // This is the heading for that lane, and for a block that mixes it with the
  // proposal lane — a block-wide claim must not say "not allowed" when no step in
  // it was forbidden, and must not be narrowed to "that change" when the block
  // also holds a step that is not a change. It reports the attempt, and claims
  // nothing about permission.
  "chat.refusedHeading.step": { en: "That step could not be completed.", ms: "Langkah itu tidak dapat diselesaikan.", zh: "该步骤未能完成。", ta: "அந்தப் படிநிலையை முடிக்க முடியவில்லை.", ar: "لم يُتمكن من إكمال تلك الخطوة." },
  // ── WHY A REFUSAL HAPPENED, in the visitor's language (t_3839995c) ─────────
  //
  // Why these exist, and why the sentence is not on the wire. A refused tool call
  // used to carry `detail` — a Malay sentence the RUNNER built in
  // api/internal/chat/chat.go — and the page printed it verbatim under the one
  // localized word above. On an English dashboard the transcript therefore read:
  //
  //   assistant tried — update_product — alat "update_product" tidak dibenarkan dalam chat ini
  //
  // The API does not receive the caller's language on a JSON call (issue #6177),
  // so anything the SERVER writes is one language for every locale — the same
  // reason the card's own wording carries keys (see "THE CARD'S OWN WORDING"). A
  // refused step now carries `detail_key` (chat.refused.*) plus `detail_params`,
  // and src/lib/chatRefusal.ts resolves it here; the wording the MODEL needs is
  // untouched, because that is the tool result, which never reaches a browser.
  //
  // The guard keeping both halves in step is src/lib/chatRefusal.test.ts: it reads
  // the Go source, extracts every "chat.refused.*" literal and fails when a key is
  // missing here in any of the five locales (or when the page prints a server
  // sentence again). Adding a key there without adding it here is not a silent
  // half-fix any more — which is exactly how this defect shipped.
  "chat.refused.writeTool": { en: "The assistant cannot run that itself. A change like that has to be prepared for you to confirm first.", ms: "Pembantu tidak boleh menjalankannya sendiri — perubahan seperti itu mesti disediakan dahulu untuk anda sahkan.", zh: "助手不能自行执行该操作——这类更改必须先准备好，由您确认。", ta: "உதவியாளர் அதைத் தானே இயக்க முடியாது — அத்தகைய மாற்றம் முதலில் உங்கள் உறுதிப்படுத்தலுக்குத் தயார் செய்யப்பட வேண்டும்.", ar: "لا يمكن للمساعد تنفيذ ذلك بنفسه — يجب تحضير هذا التغيير أولاً لتؤكده أنت." },
  "chat.refused.notAllowed": { en: "The assistant is not allowed to use that tool.", ms: "Pembantu tidak dibenarkan menggunakan alat itu.", zh: "助手无权使用该工具。", ta: "அந்தக் கருவியைப் பயன்படுத்த உதவியாளருக்கு அனுமதி இல்லை.", ar: "غير مسموح للمساعد باستخدام هذه الأداة." },
  "chat.refused.proposal": { en: "The assistant could not prepare that change, so nothing was prepared for you to confirm.", ms: "Pembantu tidak dapat menyediakan perubahan itu, jadi tiada apa-apa disediakan untuk anda sahkan.", zh: "助手无法准备该更改，因此没有可供您确认的内容。", ta: "உதவியாளரால் அந்த மாற்றத்தைத் தயார் செய்ய முடியவில்லை, எனவே உங்கள் உறுதிப்படுத்தலுக்கு எதுவும் தயார் செய்யப்படவில்லை.", ar: "لم يتمكن المساعد من تحضير هذا التغيير، لذلك لم يُعدّ شيء لتأكيده." },
  "chat.refused.proposalLimit": { en: "This answer already prepared {max} changes to confirm. Settle those first, then ask again.", ms: "Jawapan ini sudah menyediakan {max} perubahan untuk disahkan. Selesaikan yang itu dahulu, kemudian tanya lagi.", zh: "本次回答已准备了 {max} 项待确认的更改。请先处理这些，然后再提问。", ta: "இந்தப் பதில் ஏற்கனவே {max} மாற்றங்களை உறுதிப்படுத்தலுக்குத் தயார் செய்துள்ளது. அவற்றை முதலில் முடித்துவிட்டு மீண்டும் கேளுங்கள்.", ar: "أعدّ هذا الرد بالفعل {max} تغييرات للتأكيد. أنهِ تلك أولاً ثم اسأل مرة أخرى." },
  "chat.refused.tool": { en: "The tool refused the call, so nothing changed.", ms: "Alat itu menolak panggilan tersebut, jadi tiada apa-apa berubah.", zh: "该工具拒绝了此次调用，因此没有任何更改。", ta: "அந்தக் கருவி அழைப்பை மறுத்தது, எனவே எதுவும் மாறவில்லை.", ar: "رفضت الأداة هذا الطلب، فلم يتغير شيء." },
  "chat.refused.error": { en: "The call failed before it reached the tool, so nothing changed.", ms: "Panggilan itu gagal sebelum sampai ke alat, jadi tiada apa-apa berubah.", zh: "调用尚未到达工具就失败了，因此没有任何更改。", ta: "கருவியை அடையும் முன்பே அழைப்பு தோல்வியடைந்தது, எனவே எதுவும் மாறவில்லை.", ar: "فشل الطلب قبل وصوله إلى الأداة، فلم يتغير شيء." },
  // ── THE ANSWER WHEN THE MODEL WROTE NONE (t_7f1694cb) ─────────────────────
  //
  // The assistant's own bubble, not a step's detail — a different family from the
  // keys above, and the Go constant that names it (chat.go, keyReplyNoAnswer) is
  // guarded from this side by src/lib/chatAnswer.test.ts. The defect: a model whose
  // proposal was refused may end the turn having written nothing, and on prod it
  // did — the whole answer was a refusal block and an empty bubble. The runner now
  // sends this key instead of prose, because the API does not know the visitor's
  // language (issue #6177).
  //
  // It says nothing was changed (true: writes need a human Confirmation) and asks
  // for the detail the refusal was about, so the next turn is one the model can
  // actually prepare.
  "chat.reply.noAnswer": { en: "I could not finish that for you, and nothing was changed. Tell me the exact details (for a new form: its name and the fields you want) and I will try again.", ms: "Saya tidak dapat menyelesaikannya untuk anda, dan tiada apa-apa diubah. Beritahu saya butiran yang tepat — untuk borang baharu, namanya dan medan yang anda mahu — dan saya akan cuba lagi.", zh: "我无法为您完成这项操作，也没有任何更改。请告诉我具体细节——如果是新建表单，请给我表单名称和所需字段——我会再试一次。", ta: "அதை என்னால் முடிக்க முடியவில்லை, எதுவும் மாற்றப்படவில்லை. சரியான விவரங்களைச் சொல்லுங்கள் — புதிய படிவமாக இருந்தால், அதன் பெயரும் நீங்கள் விரும்பும் புலங்களும் — நான் மீண்டும் முயற்சிக்கிறேன்.", ar: "لم أتمكن من إنجاز ذلك، ولم يتغير شيء. أخبرني بالتفاصيل الدقيقة — إن كان نموذجًا جديدًا، فاسمه والحقول التي تريدها — وسأحاول مرة أخرى." },
  "chat.docs": { en: "What the assistant can and cannot do", ms: "Apa yang pembantu boleh dan tidak boleh lakukan", zh: "助手能做什么、不能做什么", ta: "உதவியாளர் என்ன செய்ய முடியும், முடியாது", ar: "ما يستطيع المساعد فعله وما لا يستطيع" },
  // A conversation is kept on the SERVER now, per person and per workspace, so
  // the sidebar needs a name for the list, a way to start a second conversation
  // without losing the first, and words for the two per-row actions.
  "chat.newConversation": { en: "New conversation", ms: "Perbualan baharu", zh: "新对话", ta: "புதிய உரையாடல்", ar: "محادثة جديدة" },
  "chat.newConversationHint": { en: "Starts a new conversation. The one you are reading stays in the history list.", ms: "Mulakan perbualan baharu. Perbualan yang sedang dibaca kekal dalam senarai sejarah.", zh: "开始新对话。您正在阅读的对话会保留在历史列表中。", ta: "புதிய உரையாடலைத் தொடங்கும். நீங்கள் படிக்கும் உரையாடல் வரலாற்றுப் பட்டியலில் இருக்கும்.", ar: "يبدأ محادثة جديدة. المحادثة التي تقرأها تبقى في قائمة السجل." },
  "chat.history": { en: "History", ms: "Sejarah", zh: "历史", ta: "வரலாறு", ar: "السجل" },
  "chat.historyEmpty": { en: "No saved conversations in this workspace yet.", ms: "Belum ada perbualan disimpan dalam ruang kerja ini.", zh: "此工作区还没有已保存的对话。", ta: "இந்தப் பணியிடத்தில் சேமிக்கப்பட்ட உரையாடல்கள் இன்னும் இல்லை.", ar: "لا توجد محادثات محفوظة في مساحة العمل هذه بعد." },
  "chat.rename": { en: "Rename", ms: "Namakan semula", zh: "重命名", ta: "மறுபெயரிடு", ar: "إعادة التسمية" },
  "chat.renameSave": { en: "Save", ms: "Simpan", zh: "保存", ta: "சேமி", ar: "حفظ" },
  "chat.delete": { en: "Delete", ms: "Padam", zh: "删除", ta: "நீக்கு", ar: "حذف" },
  "chat.deleteConfirm": { en: "Delete this conversation? Its messages are removed permanently.", ms: "Padam perbualan ini? Mesejnya akan dibuang secara kekal.", zh: "删除此对话？其中的消息将被永久移除。", ta: "இந்த உரையாடலை நீக்கவா? அதன் செய்திகள் நிரந்தரமாக அகற்றப்படும்.", ar: "حذف هذه المحادثة؟ ستُزال رسائلها نهائيًا." },
  "chat.historyLoadFail": { en: "Your saved conversations could not be loaded. You can still ask a new question.", ms: "Perbualan yang disimpan tidak dapat dimuatkan. Anda masih boleh bertanya soalan baharu.", zh: "无法加载您保存的对话。您仍然可以提出新问题。", ta: "சேமிக்கப்பட்ட உரையாடல்களை ஏற்ற முடியவில்லை. புதிய கேள்வியை இன்னும் கேட்கலாம்.", ar: "تعذّر تحميل محادثاتك المحفوظة. يمكنك طرح سؤال جديد." },
  "chat.historyCreateFail": { en: "A new conversation could not be started.", ms: "Perbualan baharu tidak dapat dimulakan.", zh: "无法开始新对话。", ta: "புதிய உரையாடலைத் தொடங்க முடியவில்லை.", ar: "تعذّر بدء محادثة جديدة." },
  "chat.historySaveFail": { en: "That change could not be saved.", ms: "Perubahan itu tidak dapat disimpan.", zh: "该更改未能保存。", ta: "அந்த மாற்றத்தைச் சேமிக்க முடியவில்லை.", ar: "تعذّر حفظ ذلك التغيير." },
  "chat.limitNote": { en: "Up to {n} messages an hour. This is a limit, not a judgement. It keeps the feature affordable for everyone.", ms: "Sehingga {n} mesej sejam. Ini had, bukan penilaian — ia menjaga kos ciri ini untuk semua orang.", zh: "每小时最多 {n} 条消息。这是限额，不是评判——它让这项功能对所有人都可负担。", ta: "மணிக்கு அதிகபட்சம் {n} செய்திகள். இது ஒரு வரம்பு, தீர்ப்பு அல்ல — அனைவருக்கும் இந்த அம்சம் மலிவாக இருக்க இது உதவுகிறது.", ar: "{n} رسالة في الساعة كحد أقصى. هذا حد وليس حكمًا — فهو يبقي هذه الميزة في متناول الجميع." },
  // Proposal confirmation (phase 2). The card is a DECISION, so the copy has to
  // say plainly that nothing has happened yet: a user who believes the change was
  // already made would not read the card at all.
  "chat.proposalBadge": { en: "Needs your confirmation", ms: "Perlu pengesahan anda", zh: "需要您确认", ta: "உங்கள் உறுதிப்படுத்தல் தேவை", ar: "يحتاج تأكيدك" },
  "chat.proposalBadgeDone": { en: "Confirmed", ms: "Disahkan", zh: "已确认", ta: "உறுதிப்படுத்தப்பட்டது", ar: "تم التأكيد" },
  "chat.proposalBadgeCancelled": { en: "Cancelled. Nothing changed", ms: "Dibatalkan — tiada apa-apa diubah", zh: "已取消 — 未作更改", ta: "ரத்து — எதுவும் மாறவில்லை", ar: "أُلغي — لم يتغير شيء" },
  "chat.proposalBadgeStale": { en: "Expired. Nothing changed", ms: "Luput — tiada apa-apa diubah", zh: "已过期 — 未作更改", ta: "காலாவதி — எதுவும் மாறவில்லை", ar: "انتهت الصلاحية — لم يتغير شيء" },
  "chat.proposalBadgeFailed": { en: "Did not go through", ms: "Tidak berjaya", zh: "未能完成", ta: "நடைபெறவில்லை", ar: "لم يكتمل" },
  // t_790b1016: a scope refusal is its OWN state, not a failure. The server
  // refused before reading the token, so nothing was attempted — the badge says
  // what the card needs rather than claiming the change failed, and it does not
  // wear the red "did not go through" pill for a request the server never ran.
  "chat.proposalBadgeScopeRequired": { en: "Needs a workspace", ms: "Perlu workspace", zh: "需要工作区", ta: "பணியிடம் தேவை", ar: "يحتاج مساحة عمل" },
  // The card's arguments are ones the tool would never accept (t_623676d1). Its
  // token is UNSPENT, so the badge must not claim expiry and must not claim a
  // failure: nothing was attempted and nothing was consumed.
  "chat.proposalBadgeUnexecutable": { en: "Cannot be applied. Nothing changed", ms: "Tidak boleh dilaksanakan — tiada apa-apa diubah", zh: "无法执行——未作更改", ta: "செயல்படுத்த முடியாது — எதுவும் மாறவில்லை", ar: "لا يمكن تنفيذه — لم يتغير شيء" },
  // ── THE CARD'S OWN WORDING (t_30ffa28a) ────────────────────────────────────
  //
  // Why these exist at all, and why they are HERE. The confirmation card's title,
  // summary, every field label and every sentence the server COMPOSED out of a
  // value ("3 fields", "yes", "no limit (infinite)") used to be Malay literals
  // built in api/internal/chat/proposal.go. The card's chrome above was already
  // localized, so on an English dashboard the same card read half English and half
  // Malay — a merchant who does not read Malay was asked to confirm a change
  // described in a language they may not read, on the one surface where the wording
  // IS the safety mechanism.
  //
  // The API does not receive the caller's language on a JSON call (see
  // lib/chatConfirm.ts / issue #6177), so anything the SERVER writes is one
  // language for every locale. The server therefore sends KEYS — chat.proposal.
  // title.*, .summary.*, .label.*, .value.* — plus the merchant's own data, and the
  // dashboard resolves them through this dictionary.
  //
  // The guard that keeps both halves in step is src/lib/chatProposal.test.ts: it
  // reads the Go source, extracts every "chat.proposal.*" literal and fails when a
  // key is missing here (or when the page prints a server sentence again). Adding a
  // key there without adding it here is not a silent half-fix any more — which is
  // exactly how this defect shipped.
  //
  // The card's TITLE, per proposable tool.
  "chat.proposal.title.update_product": { en: "Update product", ms: "Kemas kini produk", zh: "更新产品", ta: "தயாரிப்பைப் புதுப்பி", ar: "تحديث المنتج" },
  "chat.proposal.title.update_product_variation": { en: "Update product variation", ms: "Kemas kini variasi produk", zh: "更新产品变体", ta: "தயாரிப்பு மாறுபாட்டைப் புதுப்பி", ar: "تحديث نوع المنتج" },
  "chat.proposal.title.set_form_theme": { en: "Update form style", ms: "Kemas kini gaya borang", zh: "更新表单样式", ta: "படிவ பாணியைப் புதுப்பி", ar: "تحديث نمط النموذج" },
  "chat.proposal.title.upsert_form": { en: "Create or update form", ms: "Cipta atau kemas kini borang", zh: "创建或更新表单", ta: "படிவத்தை உருவாக்கு அல்லது புதுப்பி", ar: "إنشاء نموذج أو تحديثه" },
  // The card's SUMMARY, per proposable tool. upsert_form says which half is
  // happening: the same tool creates and updates, and a user confirming "update"
  // when a NEW form is about to appear has been misled.
  "chat.proposal.summary.update_product": { en: "The product fields listed below will be changed. Other fields stay as they are.", ms: "Medan produk yang disenaraikan di bawah akan ditukar. Baki medan kekal seperti sedia ada.", zh: "下面列出的产品字段将被更改，其余字段保持不变。", ta: "கீழே பட்டியலிடப்பட்ட தயாரிப்பு புலங்கள் மாற்றப்படும். மற்ற புலங்கள் அப்படியே இருக்கும்.", ar: "سيتم تغيير حقول المنتج المدرجة أدناه، وتبقى الحقول الأخرى كما هي." },
  "chat.proposal.summary.update_product_variation": { en: "The variation named below will be changed. Other variations and unlisted fields stay as they are.", ms: "Variasi yang dinamakan di bawah akan ditukar. Variasi lain dan medan yang tidak disenaraikan kekal.", zh: "下面指明的变体将被更改，其他变体和未列出的字段保持不变。", ta: "கீழே குறிப்பிடப்பட்ட மாறுபாடு மாற்றப்படும். மற்ற மாறுபாடுகளும் பட்டியலிடப்படாத புலங்களும் அப்படியே இருக்கும்.", ar: "سيتم تغيير النوع المذكور أدناه، وتبقى الأنواع الأخرى والحقول غير المدرجة كما هي." },
  "chat.proposal.summary.set_form_theme": { en: "The form's style and branding will be changed. Its content, prices and other settings do not change.", ms: "Gaya dan branding borang akan ditukar. Kandungan borang, harga dan tetapan lain tidak berubah.", zh: "表单的样式和品牌将更改，表单内容、价格和其他设置不变。", ta: "படிவத்தின் பாணியும் பிராண்டிங்கும் மாற்றப்படும். படிவ உள்ளடக்கம், விலைகள் மற்றும் பிற அமைப்புகள் மாறாது.", ar: "سيتم تغيير نمط النموذج وهويته، ولا يتغير محتواه ولا أسعاره ولا إعداداته الأخرى." },
  "chat.proposal.summary.upsert_form": { en: "A new form will be CREATED (or the existing one updated, if an id is given). Nothing is deleted, and a form created by mistake can be moved to the trash and restored.", ms: "Borang baharu akan DICIPTA (atau yang sedia ada dikemas kini, jika id diberi). Tiada apa-apa dihapuskan, dan borang yang tersilap boleh dihantar ke tong sampah dan dipulihkan.", zh: "将创建一个新表单（若给出 id，则更新现有表单）。不会删除任何内容，误创建的表单可以移入回收站并恢复。", ta: "புதிய படிவம் உருவாக்கப்படும் (id கொடுத்தால், ஏற்கனவே உள்ள ஒன்று புதுப்பிக்கப்படும்). எதுவும் நீக்கப்படாது, தவறுதலாக உருவாக்கிய படிவத்தை குப்பைக்கு நகர்த்தி மீட்டெடுக்கலாம்.", ar: "سيتم إنشاء نموذج جديد (أو تحديث النموذج الحالي إذا حُدِّد مُعرّف). لن يُحذف أي شيء، ويمكن نقل النموذج المُنشأ بالخطأ إلى سلة المهملات واستعادته." },
  // The card's FIELD LABELS, one per argument the card can list.
  "chat.proposal.label.product": { en: "Product", ms: "Produk", zh: "产品", ta: "தயாரிப்பு", ar: "المنتج" },
  "chat.proposal.label.variation": { en: "Variation", ms: "Variasi", zh: "变体", ta: "மாறுபாடு", ar: "النوع" },
  "chat.proposal.label.name": { en: "Name", ms: "Nama", zh: "名称", ta: "பெயர்", ar: "الاسم" },
  "chat.proposal.label.description": { en: "Description", ms: "Penerangan", zh: "描述", ta: "விளக்கம்", ar: "الوصف" },
  "chat.proposal.label.price": { en: "Price", ms: "Harga", zh: "价格", ta: "விலை", ar: "السعر" },
  "chat.proposal.label.stock": { en: "Stock", ms: "Stok", zh: "库存", ta: "சரக்கு", ar: "المخزون" },
  "chat.proposal.label.active": { en: "Active", ms: "Aktif", zh: "启用", ta: "செயலில்", ar: "مُفعَّل" },
  "chat.proposal.label.type": { en: "Type", ms: "Jenis", zh: "类型", ta: "வகை", ar: "الفئة" },
  "chat.proposal.label.billingPeriod": { en: "Billing period", ms: "Kitaran bil", zh: "计费周期", ta: "பில் காலம்", ar: "فترة الفوترة" },
  "chat.proposal.label.trialDays": { en: "Trial days", ms: "Hari percubaan", zh: "试用天数", ta: "சோதனை நாட்கள்", ar: "أيام التجربة" },
  "chat.proposal.label.scheduleMode": { en: "Next-charge policy", ms: "Polisi tarikh caj", zh: "下次扣费策略", ta: "அடுத்த கட்டணக் கொள்கை", ar: "سياسة الدفعة التالية" },
  "chat.proposal.label.images": { en: "Images", ms: "Gambar", zh: "图片", ta: "படங்கள்", ar: "الصور" },
  "chat.proposal.label.variations": { en: "Variations", ms: "Variasi", zh: "变体", ta: "மாறுபாடுகள்", ar: "الأنواع" },
  "chat.proposal.label.subscriptionCharges": { en: "Number of charges", ms: "Bilangan caj", zh: "扣费次数", ta: "கட்டணங்களின் எண்ணிக்கை", ar: "عدد الدفعات" },
  "chat.proposal.label.form": { en: "Form", ms: "Borang", zh: "表单", ta: "படிவம்", ar: "النموذج" },
  "chat.proposal.label.action": { en: "Action", ms: "Tindakan", zh: "操作", ta: "செயல்", ar: "الإجراء" },
  "chat.proposal.label.formTitle": { en: "Title", ms: "Tajuk", zh: "标题", ta: "தலைப்பு", ar: "العنوان" },
  "chat.proposal.label.slug": { en: "Slug (URL)", ms: "Slug (URL)", zh: "Slug（网址）", ta: "Slug (URL)", ar: "Slug (الرابط)" },
  "chat.proposal.label.config": { en: "Configuration", ms: "Konfigurasi", zh: "配置", ta: "கட்டமைப்பு", ar: "الإعدادات" },
  "chat.proposal.label.theme": { en: "Theme", ms: "Tema", zh: "主题", ta: "தீம்", ar: "السمة" },
  "chat.proposal.label.notifications": { en: "Notifications", ms: "Notifikasi", zh: "通知", ta: "அறிவிப்புகள்", ar: "الإشعارات" },
  "chat.proposal.label.chipFeeds": { en: "Payment feeds", ms: "Suapan bayaran", zh: "支付源", ta: "கட்டண ஊட்டங்கள்", ar: "تغذيات الدفع" },
  // The clear_* flags reset a variation back to inheriting the product.
  "chat.proposal.label.clearBillingPeriod": { en: "Clear the variation's billing period (inherit from the product)", ms: "Kosongkan kitaran variasi (warisi produk)", zh: "清除变体的计费周期（继承产品设置）", ta: "மாறுபாட்டின் பில் காலத்தை அழி (தயாரிப்பிலிருந்து பெறு)", ar: "مسح فترة فوترة النوع (وراثةً من المنتج)" },
  "chat.proposal.label.clearTrialDays": { en: "Clear the variation's trial days (inherit from the product)", ms: "Kosongkan hari percubaan (warisi produk)", zh: "清除变体的试用天数（继承产品设置）", ta: "மாறுபாட்டின் சோதனை நாட்களை அழி (தயாரிப்பிலிருந்து பெறு)", ar: "مسح أيام تجربة النوع (وراثةً من المنتج)" },
  "chat.proposal.label.clearSubscriptionCharges": { en: "Clear the variation's charge count (inherit from the product)", ms: "Kosongkan bilangan caj (warisi produk)", zh: "清除变体的扣费次数（继承产品设置）", ta: "மாறுபாட்டின் கட்டண எண்ணிக்கையை அழி (தயாரிப்பிலிருந்து பெறு)", ar: "مسح عدد دفعات النوع (وراثةً من المنتج)" },
  // The form theme's own keys, named the way the theme editor names them.
  "chat.proposal.label.theme.primary": { en: "Primary colour", ms: "Warna utama", zh: "主色", ta: "முதன்மை வண்ணம்", ar: "اللون الأساسي" },
  "chat.proposal.label.theme.background": { en: "Background colour", ms: "Warna latar", zh: "背景颜色", ta: "பின்னணி வண்ணம்", ar: "لون الخلفية" },
  "chat.proposal.label.theme.text": { en: "Text colour", ms: "Warna teks", zh: "文字颜色", ta: "எழுத்து வண்ணம்", ar: "لون النص" },
  "chat.proposal.label.theme.cardBackground": { en: "Card background", ms: "Latar kad", zh: "卡片背景", ta: "அட்டை பின்னணி", ar: "خلفية البطاقة" },
  "chat.proposal.label.theme.cardBorderRadius": { en: "Card corner radius", ms: "Jejari sudut kad", zh: "卡片圆角", ta: "அட்டை மூலை வளைவு", ar: "انحناء زوايا البطاقة" },
  "chat.proposal.label.theme.fontFamily": { en: "Font", ms: "Fon", zh: "字体", ta: "எழுத்துரு", ar: "الخط" },
  "chat.proposal.label.theme.logoUrl": { en: "Logo URL", ms: "URL logo", zh: "Logo 链接", ta: "லோகோ URL", ar: "رابط الشعار" },
  "chat.proposal.label.theme.layout": { en: "Layout", ms: "Susun atur", zh: "布局", ta: "அமைப்பு", ar: "التخطيط" },
  "chat.proposal.label.theme.spacing": { en: "Spacing", ms: "Jarak", zh: "间距", ta: "இடைவெளி", ar: "التباعد" },
  "chat.proposal.label.theme.productImageSize": { en: "Product image size", ms: "Saiz imej produk", zh: "产品图片尺寸", ta: "தயாரிப்பு பட அளவு", ar: "حجم صورة المنتج" },
  "chat.proposal.label.theme.productImageLayout": { en: "Product image layout", ms: "Susunan imej produk", zh: "产品图片布局", ta: "தயாரிப்பு பட அமைப்பு", ar: "تخطيط صورة المنتج" },
  "chat.proposal.label.theme.customCss": { en: "Custom CSS", ms: "CSS tersuai", zh: "自定义 CSS", ta: "தனிப்பயன் CSS", ar: "CSS مخصص" },
  "chat.proposal.label.theme.customJs": { en: "Custom JS", ms: "JS tersuai", zh: "自定义 JS", ta: "தனிப்பயன் JS", ar: "JS مخصص" },
  // The VALUES the server COMPOSES: a sentence around a number or a flag. These
  // were the last Malay on the card after the labels moved ("3 medan", "ya",
  // "tiada had (infinite)"), and they are the reason a key-only reading of the card
  // was not enough. {n} / {name} are filled from the card's value_params.
  "chat.proposal.value.yes": { en: "Yes", ms: "Ya", zh: "是", ta: "ஆம்", ar: "نعم" },
  "chat.proposal.value.no": { en: "No", ms: "Tidak", zh: "否", ta: "இல்லை", ar: "لا" },
  "chat.proposal.value.empty": { en: "(empty)", ms: "(kosong)", zh: "（空）", ta: "(காலி)", ar: "(فارغ)" },
  "chat.proposal.value.unrenderable": { en: "(cannot be shown)", ms: "(tidak dapat dipaparkan)", zh: "（无法显示）", ta: "(காட்ட முடியாது)", ar: "(تعذّر العرض)" },
  // A count of ONE has its own entry (t_212be8da). The count form interpolates
  // {n}, and English — the DEFAULT dashboard language — does not read "{n} fields"
  // correctly at n=1: the card said "Configuration — 1 fields" / "1 items" /
  // "1 times" on the very screen where the merchant is asked to confirm the change.
  // Malay, Chinese, Tamil and Arabic do not change the noun after a numeral the way
  // English does, so their singular entry repeats the words with the one written
  // out — the same shape as listOne/listMany below. The server picks between the two
  // keys (api/internal/chat/proposal.go, countedValue) and sends NO {n} with the
  // singular one, so the singular entry must never contain a placeholder.
  "chat.proposal.value.fieldCount": { en: "{n} fields", ms: "{n} medan", zh: "{n} 个字段", ta: "{n} புலங்கள்", ar: "{n} حقل" },
  "chat.proposal.value.fieldCountOne": { en: "1 field", ms: "1 medan", zh: "1 个字段", ta: "1 புலம்", ar: "1 حقل" },
  "chat.proposal.value.itemCount": { en: "{n} items", ms: "{n} item", zh: "{n} 项", ta: "{n} உருப்படிகள்", ar: "{n} عنصر" },
  "chat.proposal.value.itemCountOne": { en: "1 item", ms: "1 item", zh: "1 项", ta: "1 உருப்படி", ar: "1 عنصر" },
  "chat.proposal.value.listOne": { en: "1 item: {name}", ms: "1 item: {name}", zh: "1 项：{name}", ta: "1 உருப்படி: {name}", ar: "عنصر واحد: {name}" },
  "chat.proposal.value.listMany": { en: "{n} items: {name} …", ms: "{n} item: {name} …", zh: "{n} 项：{name} …", ta: "{n} உருப்படிகள்: {name} …", ar: "{n} عنصر: {name} …" },
  "chat.proposal.value.createForm": { en: "Create a new form", ms: "Cipta borang baharu", zh: "创建一个新表单", ta: "புதிய படிவத்தை உருவாக்கு", ar: "إنشاء نموذج جديد" },
  "chat.proposal.value.unlimitedCharges": { en: "No limit (infinite)", ms: "Tiada had (infinite)", zh: "无限制（无限）", ta: "வரம்பு இல்லை (முடிவிலி)", ar: "بلا حد (غير محدود)" },
  "chat.proposal.value.chargeCount": { en: "{n} times", ms: "{n} kali", zh: "{n} 次", ta: "{n} முறை", ar: "{n} مرات" },
  "chat.proposal.value.chargeCountOne": { en: "1 time", ms: "1 kali", zh: "1 次", ta: "1 முறை", ar: "1 مرة" },
  "chat.proposal.value.variationSet": { en: "{n} rows, the full list, replacing the existing one", ms: "{n} baris — senarai penuh, menggantikan senarai sedia ada", zh: "{n} 行——完整列表，将替换现有列表", ta: "{n} வரிசைகள் — முழுப் பட்டியல், தற்போதைய ஒன்றை மாற்றும்", ar: "{n} صفوف — القائمة الكاملة، وتستبدل القائمة الحالية" },
  "chat.proposal.value.variationSetOne": { en: "1 row, the full list, replacing the existing one", ms: "1 baris — senarai penuh, menggantikan senarai sedia ada", zh: "1 行——完整列表，将替换现有列表", ta: "1 வரிசை — முழுப் பட்டியல், தற்போதைய ஒன்றை மாற்றும்", ar: "1 صف — القائمة الكاملة، وتستبدل القائمة الحالية" },
  // ── TOOL NAMES, IN HUMAN WORDS (card t_ec87d6c7) ──────────────────────────
  // The confirmation card prints the tool it will run, and the refusal block
  // prints the tool that was refused. Both used to print the RAW name — a
  // merchant was shown `upsert_form` and `propose_write`, which are internal
  // identifiers, not language. These keys are the human name of each tool the
  // chat can DISPLAY, in all five locales.
  //
  // The list is not hand-maintained: src/lib/chatProposal.test.ts reads
  // api/internal/mcpsrv/chat_tools.go (the read allowlist and the proposable set,
  // the two lists the server's exposure comes from) and the two in-process tool
  // names, and fails when one of them has no entry here — or when an entry
  // carries an underscore. A tool added later therefore fails the suite instead of
  // appearing on the card as an identifier; at runtime an unmapped name is
  // humanised (`some_new_tool` → "Some new tool") so no snake_case can reach the
  // screen even before its translation lands.
  "chat.tool.get_account": { en: "Account details", ms: "Butiran akaun", zh: "账号详情", ta: "கணக்கு விவரங்கள்", ar: "تفاصيل الحساب" },
  "chat.tool.list_workspaces": { en: "Workspace list", ms: "Senarai ruang kerja", zh: "工作区列表", ta: "பணியிடப் பட்டியல்", ar: "قائمة مساحات العمل" },
  "chat.tool.list_forms": { en: "Form list", ms: "Senarai borang", zh: "表单列表", ta: "படிவப் பட்டியல்", ar: "قائمة النماذج" },
  "chat.tool.get_form": { en: "Form details", ms: "Butiran borang", zh: "表单详情", ta: "படிவ விவரங்கள்", ar: "تفاصيل النموذج" },
  "chat.tool.get_form_analytics": { en: "Form analytics", ms: "Analitik borang", zh: "表单分析", ta: "படிவ பகுப்பாய்வு", ar: "تحليلات النموذج" },
  "chat.tool.list_form_submissions": { en: "Form responses", ms: "Respons borang", zh: "表单回复", ta: "படிவப் பதில்கள்", ar: "ردود النموذج" },
  "chat.tool.list_products": { en: "Product list", ms: "Senarai produk", zh: "产品列表", ta: "தயாரிப்புப் பட்டியல்", ar: "قائمة المنتجات" },
  "chat.tool.list_product_variations": { en: "Product variations", ms: "Variasi produk", zh: "产品变体", ta: "தயாரிப்பு மாறுபாடுகள்", ar: "أنواع المنتج" },
  "chat.tool.get_product_analytics": { en: "Product analytics", ms: "Analitik produk", zh: "产品分析", ta: "தயாரிப்பு பகுப்பாய்வு", ar: "تحليلات المنتج" },
  "chat.tool.list_sales": { en: "Sales list", ms: "Senarai jualan", zh: "销售列表", ta: "விற்பனைப் பட்டியல்", ar: "قائمة المبيعات" },
  "chat.tool.get_sales_summary": { en: "Sales summary", ms: "Ringkasan jualan", zh: "销售汇总", ta: "விற்பனைச் சுருக்கம்", ar: "ملخص المبيعات" },
  "chat.tool.get_sale_email_status": { en: "Sale email status", ms: "Status e-mel jualan", zh: "销售邮件状态", ta: "விற்பனை மின்னஞ்சல் நிலை", ar: "حالة بريد البيع" },
  "chat.tool.list_subscriptions": { en: "Subscription list", ms: "Senarai langganan", zh: "订阅列表", ta: "சந்தாப் பட்டியல்", ar: "قائمة الاشتراكات" },
  "chat.tool.list_email_deliveries": { en: "Email deliveries", ms: "Penghantaran e-mel", zh: "邮件送达", ta: "மின்னஞ்சல் அனுப்புகைகள்", ar: "تسليمات البريد" },
  "chat.tool.list_spam_sales": { en: "Spam sales", ms: "Jualan spam", zh: "垃圾销售", ta: "ஸ்பேம் விற்பனைகள்", ar: "مبيعات مزعجة" },
  "chat.tool.list_spam_submissions": { en: "Spam responses", ms: "Respons spam", zh: "垃圾回复", ta: "ஸ்பேம் பதில்கள்", ar: "ردود مزعجة" },
  "chat.tool.list_media": { en: "Media library", ms: "Pustaka media", zh: "媒体库", ta: "ஊடக நூலகம்", ar: "مكتبة الوسائط" },
  "chat.tool.list_events": { en: "Activity log", ms: "Log aktiviti", zh: "活动日志", ta: "செயல்பாட்டுப் பதிவு", ar: "سجل النشاط" },
  "chat.tool.list_templates": { en: "Template list", ms: "Senarai templat", zh: "模板列表", ta: "வார்ப்புருப் பட்டியல்", ar: "قائمة القوالب" },
  "chat.tool.list_own_templates": { en: "Your templates", ms: "Templat anda", zh: "您的模板", ta: "உங்கள் வார்ப்புருக்கள்", ar: "قوالبك" },
  "chat.tool.list_feedback": { en: "Feedback threads", ms: "Thread maklum balas", zh: "反馈会话", ta: "கருத்து நூல்கள்", ar: "محادثات الملاحظات" },
  "chat.tool.get_feedback_thread": { en: "One feedback thread", ms: "Satu thread maklum balas", zh: "单个反馈会话", ta: "ஒரு கருத்து நூல்", ar: "محادثة ملاحظات واحدة" },
  // The four SETTINGS reads (mcpsrv.chatReadTools). The assistant answers questions
  // about how this merchant's account is configured as well as about their records,
  // so these names reach the refusal block like any other read tool and need words
  // in every locale for the same reason: the merchant must never be shown
  // `get_payment_fees`.
  "chat.tool.get_workspace_settings": { en: "Workspace settings", ms: "Tetapan ruang kerja", zh: "工作区设置", ta: "பணியிட அமைப்புகள்", ar: "إعدادات مساحة العمل" },
  "chat.tool.get_payment_fees": { en: "Platform fee settings", ms: "Tetapan caj platform", zh: "平台费设置", ta: "தளக் கட்டண அமைப்புகள்", ar: "إعدادات رسوم المنصة" },
  "chat.tool.get_shipping_rates": { en: "Shipping rates", ms: "Kadar penghantaran", zh: "运费费率", ta: "ஷிப்பிங் கட்டணங்கள்", ar: "أسعار الشحن" },
  "chat.tool.get_shipping_couriers": { en: "Courier connections", ms: "Sambungan kurier", zh: "快递连接", ta: "கூரியர் இணைப்புகள்", ar: "اتصالات شركات الشحن" },
  // The four WRITE tools the chat may propose (mcpsrv.chatProposableTools).
  // upsert_form's label is "Form save" rather than the card's own title ("Create
  // or update form"): the title is rendered three lines above the footnote, and a
  // footnote that repeats it tells the merchant nothing.
  "chat.tool.update_product": { en: "Product update", ms: "Kemas kini produk", zh: "产品更新", ta: "தயாரிப்புப் புதுப்பிப்பு", ar: "تحديث منتج" },
  "chat.tool.update_product_variation": { en: "Product variation update", ms: "Kemas kini variasi produk", zh: "产品变体更新", ta: "தயாரிப்பு மாறுபாடு புதுப்பிப்பு", ar: "تحديث نوع منتج" },
  "chat.tool.set_form_theme": { en: "Form style update", ms: "Kemas kini gaya borang", zh: "表单样式更新", ta: "படிவ பாணி புதுப்பிப்பு", ar: "تحديث نمط النموذج" },
  "chat.tool.upsert_form": { en: "Form save", ms: "Simpan borang", zh: "保存表单", ta: "படிவத்தைச் சேமி", ar: "حفظ النموذج" },
  // The chat's two in-process tools, which appear in the refusal trace.
  "chat.tool.propose_write": { en: "Change proposal", ms: "Cadangan perubahan", zh: "更改提议", ta: "மாற்ற முன்மொழிவு", ar: "مقترح تغيير" },
  "chat.tool.submit_feedback": { en: "Feedback filing", ms: "Penghantaran maklum balas", zh: "提交反馈", ta: "கருத்துச் சமர்ப்பிப்பு", ar: "إرسال ملاحظات" },
  "chat.confirm": { en: "Confirm", ms: "Sahkan", zh: "确认", ta: "உறுதிப்படுத்து", ar: "تأكيد" },
  "chat.cancel": { en: "Cancel", ms: "Batal", zh: "取消", ta: "ரத்து", ar: "إلغاء" },
  "chat.confirmHint": { en: "Nothing has changed yet.", ms: "Belum ada apa-apa yang berubah.", zh: "目前尚未发生任何更改。", ta: "இதுவரை எதுவும் மாறவில்லை.", ar: "لم يتغير شيء بعد." },
  "chat.confirming": { en: "Applying the change…", ms: "Sedang melaksanakan perubahan…", zh: "正在应用更改…", ta: "மாற்றம் பயன்படுத்தப்படுகிறது…", ar: "جارٍ تطبيق التغيير…" },
  "chat.cancelling": { en: "Discarding the proposal…", ms: "Sedang membuang cadangan…", zh: "正在丢弃该提议…", ta: "முன்மொழிவு நீக்கப்படுகிறது…", ar: "جارٍ تجاهل المقترح…" },
  "chat.confirmed": { en: "Done. The change has been applied.", ms: "Selesai — perubahan telah dilaksanakan.", zh: "完成——更改已生效。", ta: "முடிந்தது — மாற்றம் பயன்படுத்தப்பட்டது.", ar: "تم — طُبِّق التغيير." },
  "chat.cancelled": { en: "Cancelled. Nothing was changed.", ms: "Dibatalkan — tiada apa-apa yang diubah.", zh: "已取消——未做任何更改。", ta: "ரத்து செய்யப்பட்டது — எதுவும் மாற்றப்படவில்லை.", ar: "أُلغي — لم يتغير شيء." },
  "chat.proposalStale": { en: "This proposal has expired or was already used. Ask the assistant to prepare it again.", ms: "Cadangan ini telah luput atau sudah digunakan. Minta pembantu menyediakannya semula.", zh: "该提议已过期或已被使用。请让助手重新准备。", ta: "இந்த முன்மொழிவு காலாவதியானது அல்லது ஏற்கனவே பயன்படுத்தப்பட்டது. உதவியாளரை மீண்டும் தயார் செய்யச் சொல்லுங்கள்.", ar: "انتهت صلاحية هذا المقترح أو استُخدم سابقًا. اطلب من المساعد تحضيره من جديد." },
  // A card whose arguments the tool would never accept (t_623676d1). Distinct from
  // chat.proposalStale on purpose: this card did NOT expire and was NOT used (the
  // server refuses it before claiming the token), so telling the user it "expired
  // or was already used" is false and hides the useful fact that a fresh card is
  // all they need — the sentence says that instead.
  "chat.proposalUnexecutable": { en: "This proposal cannot be carried out as prepared. Ask the assistant to prepare it again. Nothing was changed and nothing was used up.", ms: "Cadangan ini tidak boleh dilaksanakan seperti yang disediakan. Minta pembantu menyediakannya semula — tiada apa-apa diubah dan tiada apa-apa digunakan.", zh: "该提议按当前内容无法执行。请让助手重新准备——未做任何更改，也未消耗任何东西。", ta: "தயார் செய்யப்பட்டபடி இந்த முன்மொழிவை நிறைவேற்ற முடியாது. உதவியாளரை மீண்டும் தயார் செய்யச் சொல்லுங்கள் — எதுவும் மாறவில்லை, எதுவும் செலவாகவில்லை.", ar: "لا يمكن تنفيذ هذا المقترح بصيغته المُعدّة. اطلب من المساعد تحضيره من جديد — لم يتغير شيء ولم يُستهلك شيء." },
  "chat.cancelStale": { en: "That proposal is already settled. There is nothing to cancel.", ms: "Cadangan itu sudah selesai — tiada apa untuk dibatalkan.", zh: "该提议已结案——无可取消。", ta: "அந்த முன்மொழிவு ஏற்கனவே முடிந்தது — ரத்து செய்ய ஒன்றுமில்லை.", ar: "هذا المقترح محسوم بالفعل — لا شيء لإلغائه." },
  "chat.confirmFail": { en: "The change could not be applied. Please try again.", ms: "Perubahan tidak dapat dilaksanakan. Sila cuba lagi.", zh: "无法应用该更改。请重试。", ta: "மாற்றத்தைப் பயன்படுத்த முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تطبيق التغيير. يرجى المحاولة مرة أخرى." },
  // A confirmed write the workspace ROLE refused (t_1900adef). Its OWN sentence,
  // in its own state, because BOTH halves of chat.confirmFail are wrong for it:
  // the retry advice cannot work (the refusal arrives AFTER the token is claimed,
  // so the card is spent) and a generic sentence names no cause when the server
  // gave one — "you need the manage or admin role here". The sentence therefore
  // states the cause, that the change did not go through, and that the card cannot
  // be reused, and offers the only remedy that exists (a fresh card).
  //
  // It is deliberately NOT the shared apiErr.manageRequired sentence (which names
  // the role but not the card's state): the two are different questions, and this
  // one has to answer the second.
  "chat.confirmRoleDenied": { en: "This change was not applied because you need the manage or admin role in this workspace, and this card cannot be used again. Ask the assistant to prepare a new one.", ms: "Perubahan ini tidak dilaksanakan kerana anda perlukan peranan manage atau admin dalam workspace ini, dan kad ini tidak boleh digunakan lagi — minta pembantu menyediakan kad baharu.", zh: "由于您需要此工作区的 manage 或 admin 角色，该更改未被执行，而且这张卡片不能再次使用——请让助手重新准备一张。", ta: "இந்த பணியிடத்தில் manage அல்லது admin பங்கு தேவை என்பதால் இந்த மாற்றம் நிறைவேற்றப்படவில்லை, மேலும் இந்த அட்டையை மீண்டும் பயன்படுத்த முடியாது — உதவியாளரை புதிய அட்டை தயார் செய்யச் சொல்லுங்கள்.", ar: "لم يُطبَّق هذا التغيير لأنك تحتاج إلى دور manage أو admin في مساحة العمل هذه، ولا يمكن استخدام هذه البطاقة مرة أخرى — اطلب من المساعد تحضير بطاقة جديدة." },
  "chat.cancelFail": { en: "The proposal could not be cancelled. Please try again.", ms: "Cadangan tidak dapat dibatalkan. Sila cuba lagi.", zh: "无法取消该提议。请重试。", ta: "முன்மொழிவை ரத்து செய்ய முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر إلغاء المقترح. يرجى المحاولة مرة أخرى." },
  // A confirmed change the server REFUSED and this build cannot classify
  // (t_bf4feea2). Its own sentence because chat.confirmFail is wrong for it: the
  // refusal arrives AFTER the token is claimed, so the card is spent and "please
  // try again" invites a click that cannot work. It says what is true — nothing was
  // applied, the card is finished — and offers the only remedy there is.
  "chat.confirmRefused": { en: "The change was not carried out, and this card cannot be used again. Ask the assistant to prepare a new card.", ms: "Perubahan tidak dilaksanakan, dan kad ini tidak boleh digunakan lagi — minta pembantu menyediakan kad baharu.", zh: "该更改未被执行，而且这张卡片不能再次使用——请让助手重新准备一张卡片。", ta: "இந்த மாற்றம் நிறைவேற்றப்படவில்லை, மேலும் இந்த அட்டையை மீண்டும் பயன்படுத்த முடியாது — உதவியாளரை புதிய அட்டை தயார் செய்யச் சொல்லுங்கள்.", ar: "لم يُنفَّذ هذا التغيير، ولا يمكن استخدام هذه البطاقة مرة أخرى — اطلب من المساعد تحضير بطاقة جديدة." },
  // A confirmed form creation refused because the ACCOUNT has no workspace at all
  // (t_bf4feea2, measured on staging: a magic-link account never gets one). It is
  // NOT the shared apiErr.workspaceRequired sentence ("Create a workspace before
  // creating a form") because that one answers the create DOOR, where the input is
  // still in the user's hands; here the card is already spent and the user has to
  // know both halves — why nothing happened, and that a new card is needed after.
  "chat.confirmWorkspaceRequired": { en: "This change was not carried out because this account has no workspace yet, and this card cannot be used again. Create a workspace, then ask the assistant to prepare a new card.", ms: "Perubahan ini tidak dilaksanakan kerana akaun ini belum mempunyai workspace, dan kad ini tidak boleh digunakan lagi — buat workspace dahulu, kemudian minta pembantu menyediakan kad baharu.", zh: "该更改未被执行，因为这个账号还没有工作区，而且这张卡片不能再次使用——请先创建工作区，再让助手重新准备一张卡片。", ta: "இந்தக் கணக்கிற்கு இன்னும் பணியிடம் இல்லாததால் இந்த மாற்றம் நிறைவேற்றப்படவில்லை, மேலும் இந்த அட்டையை மீண்டும் பயன்படுத்த முடியாது — முதலில் ஒரு பணியிடத்தை உருவாக்கி, பிறகு உதவியாளரை புதிய அட்டை தயார் செய்யச் சொல்லுங்கள்.", ar: "لم يُنفَّذ هذا التغيير لأن هذا الحساب ليس لديه مساحة عمل بعد، ولا يمكن استخدام هذه البطاقة مرة أخرى — أنشئ مساحة عمل أولًا، ثم اطلب من المساعد تحضير بطاقة جديدة." },
  "chat.confirmRateLimited": { en: "You have reached the confirmation limit (30 an hour). Please try again shortly.", ms: "Anda telah mencapai had pengesahan (30 sejam). Sila cuba sebentar lagi.", zh: "您已达到确认上限（每小时 30 次）。请稍后再试。", ta: "உறுதிப்படுத்தல் வரம்பை அடைந்துவிட்டீர்கள் (மணிக்கு 30). சிறிது நேரத்தில் மீண்டும் முயற்சிக்கவும்.", ar: "بلغت حد التأكيد (30 في الساعة). يرجى المحاولة بعد قليل." },
  // The chat calls carry the selected workspace (?workspace=). When a
  // multi-workspace session reaches an endpoint without it, the server refuses
  // rather than answer about the user's first account — and this is the copy that
  // tells the user what to do about it. It is NOT the "session expired" path: the
  // server answers 400 for a missing scope, not 401/403, so the page does not
  // redirect to /login for a request the user never made by hand.
  "chat.scopeRequired": { en: "Choose a workspace first. The assistant answers for one workspace at a time.", ms: "Pilih workspace dahulu — pembantu menjawab untuk satu workspace pada satu masa.", zh: "请先选择工作区——助手一次只回答一个工作区的问题。", ta: "முதலில் ஒரு பணியிடத்தைத் தேர்ந்தெடுங்கள் — உதவியாளர் ஒரு நேரத்தில் ஒரு பணியிடத்திற்கே பதிலளிக்கும்.", ar: "اختر مساحة عمل أولاً — المساعد يجيب عن مساحة عمل واحدة في كل مرة." },

  // admin feedback triage
  "afb.title": { en: "Feedback triage", ms: "Triaj maklum balas", zh: "反馈分类", ta: "கருத்து வகைப்படுத்தல்", ar: "فرز الملاحظات" },
  "afb.subtitle": { en: "Every thread filed by any user. Reply in the thread and set its status.", ms: "Setiap thread yang dihantar oleh mana-mana pengguna. Balas dalam thread dan tetapkan statusnya.", zh: "所有用户提交的对话。在对话中回复并设置状态。", ta: "எந்தப் பயனரும் சமர்ப்பித்த ஒவ்வொரு உரையாடலும். உரையாடலில் பதிலளித்து நிலையை அமைக்கவும்.", ar: "كل محادثة أرسلها أي مستخدم. ردّ في المحادثة وحدّد حالتها." },
  "afb.loadFail": { en: "We couldn't load the feedback queue. Please try again.", ms: "Kami tidak dapat memuatkan baris gilir maklum balas. Sila cuba lagi.", zh: "无法加载反馈队列。请重试。", ta: "கருத்து வரிசையை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل قائمة الملاحظات. يرجى المحاولة مرة أخرى." },
  "afb.filterAll": { en: "All", ms: "Semua", zh: "全部", ta: "அனைத்தும்", ar: "الكل" },
  "afb.replyTitle": { en: "Reply as JomForm", ms: "Balas sebagai JomForm", zh: "以 JomForm 身份回复", ta: "JomForm ஆக பதிலளி", ar: "الرد باسم JomForm" },
  "afb.replyPh": { en: "Write your reply…", ms: "Tulis balasan anda…", zh: "写下您的回复…", ta: "உங்கள் பதிலை எழுதவும்…", ar: "اكتب ردك…" },
  "afb.replySend": { en: "Send reply", ms: "Hantar balasan", zh: "发送回复", ta: "பதிலை அனுப்பு", ar: "إرسال الرد" },
  "afb.replied": { en: "Reply sent. The user has been notified.", ms: "Balasan dihantar — pengguna telah dimaklumkan.", zh: "回复已发送——已通知用户。", ta: "பதில் அனுப்பப்பட்டது — பயனருக்கு அறிவிக்கப்பட்டது.", ar: "تم إرسال الرد — وتم إخطار المستخدم." },
  "afb.replyFail": { en: "We couldn't send that reply. Please try again.", ms: "Kami tidak dapat menghantar balasan itu. Sila cuba lagi.", zh: "无法发送该回复。请重试。", ta: "அந்தப் பதிலை அனுப்ப முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر إرسال هذا الرد. يرجى المحاولة مرة أخرى." },
  "afb.statusSet": { en: "Status updated.", ms: "Status dikemas kini.", zh: "状态已更新。", ta: "நிலை புதுப்பிக்கப்பட்டது.", ar: "تم تحديث الحالة." },
  "afb.statusFail": { en: "We couldn't change the status. Please try again.", ms: "Kami tidak dapat menukar status. Sila cuba lagi.", zh: "无法更改状态。请重试。", ta: "நிலையை மாற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تغيير الحالة. يرجى المحاولة مرة أخرى." },
  "afb.empty": { en: "No feedback threads match this filter.", ms: "Tiada thread maklum balas sepadan dengan penapis ini.", zh: "没有符合此筛选条件的反馈对话。", ta: "இந்த வடிகட்டலுடன் பொருந்தும் உரையாடல் இல்லை.", ar: "لا توجد محادثات ملاحظات تطابق هذا المُرشِّح." },
  "afb.columnFiler": { en: "Filer", ms: "Penghantar", zh: "提交者", ta: "சமர்ப்பித்தவர்", ar: "المُرسِل" },
  "afb.columnStatus": { en: "Status", ms: "Status", zh: "状态", ta: "நிலை", ar: "الحالة" },
  "afb.columnActivity": { en: "Last activity", ms: "Aktiviti terakhir", zh: "最近活动", ta: "கடைசி செயல்பாடு", ar: "آخر نشاط" },
  "afb.columnMessages": { en: "Messages", ms: "Mesej", zh: "消息", ta: "செய்திகள்", ar: "الرسائل" },
  "afb.titleColumn": { en: "Subject", ms: "Tajuk", zh: "主题", ta: "தலைப்பு", ar: "الموضوع" },

  // /docs index chrome (t_d768342e): the category labels and the page's own
  // headings. The docs PROSE stays English (every /docs page is static English,
  // like the legal pages), but the taxonomy a visitor navigates by is part of the
  // app chrome, so it follows the reader's language like everything else here.
  "docs.title": { en: "JomForm Docs", ms: "Dokumentasi JomForm", zh: "JomForm 文档", ta: "JomForm ஆவணங்கள்", ar: "وثائق JomForm" },
  "docs.intro": { en: "Everything JomForm does, one line per feature, grouped by what you are trying to get done. Where the full walk-through exists, the entry links to it.", ms: "Semua yang JomForm lakukan, satu baris setiap ciri, dikumpulkan mengikut apa yang anda mahu lakukan. Jika panduan penuh wujud, entri itu berpaut kepadanya.", zh: "JomForm 的每一项功能一句话说明，按您想完成的事分组。若存在完整指南，条目会链接过去。", ta: "JomForm செய்யும் அனைத்தும், ஒரு அம்சத்திற்கு ஒரு வரி, நீங்கள் செய்ய விரும்புவதைப் பொறுத்து தொகுக்கப்பட்டது. முழு வழிகாட்டி இருந்தால், அந்த உருப்படி அதற்கு இணைக்கிறது.", ar: "كل ما يفعله JomForm، سطر واحد لكل ميزة، مجمّعة حسب ما تريد إنجازه. وحين يوجد دليل كامل، يرتبط البند به." },
  "docs.gsTitle": { en: "Start here", ms: "Mula di sini", zh: "从这里开始", ta: "இங்கே தொடங்குங்கள்", ar: "ابدأ من هنا" },
  // Card t_ff973d0b: the label over the reference-shortcut row. It is a group
  // label, not a heading a reader reads as page structure, so the /docs index
  // renders it small and uppercase over the chips.
  "docs.popularTitle": { en: "Popular references", ms: "Rujukan popular", zh: "常用参考", ta: "பிரபலமான குறிப்புகள்", ar: "مراجع شائعة" },
  "docs.featuresTitle": { en: "Features", ms: "Ciri-ciri", zh: "功能", ta: "அம்சங்கள்", ar: "الميزات" },
  "docs.faqTitle": { en: "FAQ", ms: "Soalan lazim", zh: "常见问题", ta: "அடிக்கடி கேட்கப்படும் கேள்விகள்", ar: "الأسئلة الشائعة" },
  "docs.next": { en: "Full guide →", ms: "Panduan penuh →", zh: "完整指南 →", ta: "முழு வழிகாட்டி →", ar: "الدليل الكامل →" },
  // t_5425c958: the "Workspaces" category on the /docs index. It arrived with the
  // workspaces guide the rebase added.
  "docs.catWorkspaces": { en: "Workspaces", ms: "Workspace", zh: "工作区", ta: "பணியிடங்கள்", ar: "مساحات العمل" },
  "docs.catSales": { en: "Sales & payments", ms: "Jualan & bayaran", zh: "销售与收款", ta: "விற்பனை & பணம்", ar: "المبيعات والمدفوعات" },
  "docs.catForms": { en: "Forms & fields", ms: "Borang & medan", zh: "表单与字段", ta: "படிவங்கள் & புலங்கள்", ar: "النماذج والحقول" },
  "docs.catStorefront": { en: "Storefront & pages", ms: "Kedai & halaman", zh: "店铺与页面", ta: "கடை & பக்கங்கள்", ar: "المتجر والصفحات" },
  "docs.catEmail": { en: "Email", ms: "Emel", zh: "邮件", ta: "மின்னஞ்சல்", ar: "البريد الإلكتروني" },
  "docs.catGrowth": { en: "Growth", ms: "Pertumbuhan", zh: "增长", ta: "வளர்ச்சி", ar: "النمو" },
  "docs.catSecurity": { en: "Security", ms: "Keselamatan", zh: "安全", ta: "பாதுகாப்பு", ar: "الأمان" },
  "docs.catDev": { en: "For developers", ms: "Untuk pembangun", zh: "开发者", ta: "டெவலப்பர்களுக்கு", ar: "للمطوّرين" },
  "docs.linkPlaceholders": { en: "Placeholders reference", ms: "Rujukan placeholder", zh: "占位符参考", ta: "இடமாற்றி குறிப்பு", ar: "مرجع العناصر النائبة" },
  "docs.linkMCP": { en: "MCP tool reference", ms: "Rujukan alat MCP", zh: "MCP 工具参考", ta: "MCP கருவி குறிப்பு", ar: "مرجع أدوات MCP" },
  // Card t_ff973d0b: the chip that carried this label is gone from the index —
  // /docs/embed is an ENTRY on the page with the same title and a sidebar row —
  // so the key went with it. ("Design → AI agent → MCP guide" left for the same
  // reason: an entry and a sidebar row already carry that destination.)
  // Card t_7b9f6d18: the screenshot guides (docs/guides/<slug>) now reach the
  // index through START_LINKS ("Start here"), not through the shortcut row.
  "docs.linkGuides": { en: "Screenshot guides", ms: "Panduan bergambar", zh: "图文指南", ta: "பட வழிகாட்டிகள்", ar: "أدلة مصوّرة" },
  // t_5425c958: the workspace guide is reached from its own two entries on the
  // index (which render `docs.next`), not from the quick-link row — so it needs
  // no label key here. Only keys with a real call site are declared.
  "common.cancel": { en: "Cancel", ms: "Batal", zh: "取消", ta: "ரத்துசெய்", ar: "إلغاء" },
  "common.saving": { en: "Working…", ms: "Sedang diproses…", zh: "处理中…", ta: "செயலாக்கப்படுகிறது…", ar: "جارٍ التنفيذ…" },
  // Workspace tab (Settings): workspace identity + the Danger zone.
  "set.tab.workspace": { en: "Workspace", ms: "Workspace", zh: "工作区", ta: "பணியிடம்", ar: "مساحة العمل" },
  "ws.identityTitle": { en: "Workspace", ms: "Workspace", zh: "工作区", ta: "பணியிடம்", ar: "مساحة العمل" },
  "ws.identityHelp": { en: "The name and address of this workspace. Forms, products and sales all belong to it.", ms: "Nama dan alamat workspace ini. Borang, produk dan jualan semuanya miliknya.", zh: "此工作区的名称和地址。表单、商品和销售都属于它。", ta: "இந்தப் பணியிடத்தின் பெயரும் முகவரியும். படிவங்கள், தயாரிப்புகள், விற்பனைகள் அனைத்தும் இதற்குச் சொந்தம்.", ar: "اسم مساحة العمل هذه وعنوانها. تنتمي إليها النماذج والمنتجات والمبيعات." },
  "ws.name": { en: "Name", ms: "Nama", zh: "名称", ta: "பெயர்", ar: "الاسم" },
  "ws.slug": { en: "Address", ms: "Alamat", zh: "地址", ta: "முகவரி", ar: "العنوان" },
  "ws.renameNote": { en: "You can rename it below. Changing the address keeps the old one working, so links, embeds and QR codes already in use keep pointing here.", ms: "Anda boleh menukar namanya di bawah. Menukar alamat mengekalkan alamat lama berfungsi — pautan, embed dan kod QR yang sudah digunakan tetap menuju ke sini.", zh: "您可以在下方重命名。更改地址后旧地址仍然有效——已使用的链接、嵌入和二维码仍会指向此处。", ta: "கீழே மறுபெயரிடலாம். முகவரியை மாற்றினாலும் பழைய முகவரி வேலை செய்யும் — ஏற்கனவே பயன்படுத்தப்படும் இணைப்புகள், embed மற்றும் QR குறியீடுகள் இங்கேயே செல்லும்.", ar: "يمكنك إعادة التسمية أدناه. تغيير العنوان يُبقي العنوان القديم يعمل — فالروابط والتضمينات ورموز QR المستخدمة تبقى تشير إلى هنا." },
  // ── Rename (name + public slug) ──
  // The name is a label the merchant's colleagues read; the SLUG is the tenant's
  // public host ({slug}.jomform.com) — printed on QR stands, pasted as an iframe
  // embed and handed to affiliates. That asymmetry is why the two fields are
  // gated differently below, and the copy says so rather than leaving a disabled
  // control unexplained.
  "ws.renameTitle": { en: "Rename this workspace", ms: "Tukar nama workspace ini", zh: "重命名此工作区", ta: "இந்தப் பணியிடத்தை மறுபெயரிடு", ar: "إعادة تسمية مساحة العمل هذه" },
  "ws.renameHelp": { en: "The name is what you and your team see. The address is the public link people use.", ms: "Nama ialah apa yang anda dan pasukan anda lihat. Alamat ialah pautan awam yang orang guna.", zh: "名称是您和团队看到的。地址是公众使用的公开链接。", ta: "பெயர் என்பது நீங்களும் உங்கள் குழுவும் பார்ப்பது. முகவரி என்பது மக்கள் பயன்படுத்தும் பொதுவான இணைப்பு.", ar: "الاسم هو ما تراه أنت وفريقك. العنوان هو الرابط العام الذي يستخدمه الناس." },
  "ws.nameField": { en: "Name", ms: "Nama", zh: "名称", ta: "பெயர்", ar: "الاسم" },
  "ws.slugField": { en: "Address", ms: "Alamat", zh: "地址", ta: "முகவரி", ar: "العنوان" },
  "ws.saveRename": { en: "Save", ms: "Simpan", zh: "保存", ta: "சேமி", ar: "حفظ" },
  "ws.renameSaved": { en: "Saved.", ms: "Disimpan.", zh: "已保存。", ta: "சேமிக்கப்பட்டது.", ar: "تم الحفظ." },
  "ws.renameFail": { en: "Could not rename the workspace.", ms: "Tidak dapat menukar nama workspace.", zh: "无法重命名工作区。", ta: "பணியிடத்தை மறுபெயரிட முடியவில்லை.", ar: "تعذّر إعادة تسمية مساحة العمل." },
  "ws.renameNothing": { en: "Nothing changed yet. Edit the name or the address first.", ms: "Belum ada perubahan — ubah nama atau alamat dahulu.", zh: "尚无更改——请先修改名称或地址。", ta: "இன்னும் மாற்றம் இல்லை — முதலில் பெயரையோ முகவரியையோ மாற்றவும்.", ar: "لا تغيير بعد — عدّل الاسم أو العنوان أولاً." },
  "ws.slugInvalid": { en: "The address must be 3-40 characters: a-z, 0-9 and dashes only, and it cannot end in \"jomform\".", ms: "Alamat mesti 3-40 aksara: a-z, 0-9 dan sengkang sahaja, dan tidak boleh berakhir dengan \"jomform\".", zh: "地址必须为 3-40 个字符：仅限 a-z、0-9 和连字符，且不能以 \"jomform\" 结尾。", ta: "முகவரி 3-40 எழுத்துகள்: a-z, 0-9 மற்றும் கோடு மட்டும்; \"jomform\" என முடியக்கூடாது.", ar: "يجب أن يكون العنوان من 3 إلى 40 حرفًا: a-z و0-9 والشرطات فقط، ولا ينتهي بـ \"jomform\"." },
  "ws.slugLocked": { en: "Only the workspace owner can change the address. It is the public link printed in QR codes, embeds and affiliate links, so it is the owner's call. Ask them, or create a new workspace if you need a different one.", ms: "Hanya pemilik workspace boleh menukar alamat. Ia pautan awam yang tercetak dalam kod QR, embed dan pautan affiliate, jadi ia keputusan pemilik — mintalah pemilik, atau cipta workspace baharu jika anda perlukan alamat lain.", zh: "只有工作区所有者可以更改地址。它是印在二维码、嵌入和联盟链接中的公开链接，因此由所有者决定——请询问所有者，或创建新的工作区。", ta: "பணியிட உரிமையாளர் மட்டுமே முகவரியை மாற்றலாம். QR குறியீடுகள், embed மற்றும் affiliate இணைப்புகளில் அச்சிடப்படும் பொது இணைப்பு இது — எனவே உரிமையாளரின் முடிவு. அவரிடம் கேளுங்கள், அல்லது புதிய பணியிடம் உருவாக்குங்கள்.", ar: "المالك فقط يمكنه تغيير العنوان. فهو الرابط العام المطبوع في رموز QR والتضمينات وروابط الإحالة، لذا القرار للمالك — اطلبه منه، أو أنشئ مساحة عمل جديدة." },
  "ws.slugKeepsOld": { en: "The old link keeps working: it redirects to the new address, so printed QR codes and shared links do not break.", ms: "Pautan lama tetap berfungsi: ia mengalih ke alamat baharu, jadi kod QR yang dicetak dan pautan yang dikongsi tidak rosak.", zh: "旧链接仍然有效：它会重定向到新地址，因此已打印的二维码和共享链接不会失效。", ta: "பழைய இணைப்பு தொடர்ந்து வேலை செய்யும்: அது புதிய முகவரிக்கு திருப்பி விடும், எனவே அச்சிட்ட QR குறியீடுகளும் பகிரப்பட்ட இணைப்புகளும் உடையாது.", ar: "الرابط القديم يبقى يعمل: يعيد التوجيه إلى العنوان الجديد، فلا تتعطل رموز QR المطبوعة والروابط المشارَكة." },
  "ws.renameDocs": { en: "How renaming works", ms: "Cara tukar nama berfungsi", zh: "重命名的工作方式", ta: "மறுபெயரிடல் எப்படி வேலை செய்கிறது", ar: "كيف تعمل إعادة التسمية" },
  "ws.dangerTitle": { en: "Danger zone", ms: "Zon bahaya", zh: "危险区域", ta: "ஆபத்து மண்டலம்", ar: "منطقة الخطر" },
  "ws.dangerHelp": { en: "Moving a workspace to the Trash destroys nothing: its forms, products, sales and payment account all stay, and you can restore it any time. Deleting it for good is a separate step reached through the REST API or an MCP client acting for the owner, and never from the dashboard, which offers Restore only.", ms: "Menghantar workspace ke Tong Sampah tidak memusnahkan apa-apa: borang, produk, jualan dan akaun pembayarannya semuanya kekal, dan anda boleh pulihkannya bila-bila masa. Pemadaman kekal ialah langkah berasingan yang dicapai melalui REST API atau klien MCP bagi pihak pemilik — bukan dari papan pemuka, yang hanya menawarkan Pulihkan.", zh: "将工作区移入回收站不会销毁任何内容：其表单、商品、销售及收款账户都会保留，您可以随时恢复。永久删除是单独的操作，需通过 REST API 或以所有者身份运行的 MCP 客户端执行——不能在面板中完成，面板只提供还原。", ta: "பணியிடத்தைக் குப்பைத் தொட்டிக்கு நகர்த்துவது எதையும் அழிக்காது: படிவங்கள், தயாரிப்புகள், விற்பனைகள் மற்றும் கட்டணக் கணக்கு அனைத்தும் இருக்கும், எப்போது வேண்டுமானாலும் மீட்டெடுக்கலாம். நிரந்தரமாக நீக்குவது தனி செயல் — REST API அல்லது உரிமையாளருக்காக இயங்கும் MCP கிளையன் வழியாக மட்டுமே. டாஷ்போர்டில் இல்லை; அது மீட்டெடுப்பை மட்டுமே வழங்குகிறது.", ar: "نقل مساحة العمل إلى سلة المهملات لا يُتلف شيئاً: تبقى نماذجها ومنتجاتها ومبيعاتها وحساب الدفع الخاص بها، ويمكنك استعادتها في أي وقت. أما الحذف النهائي فهو إجراء منفصل يتم عبر REST API أو عميل MCP نيابة عن المالك — وليس من لوحة التحكم، فهي توفر الاستعادة فقط." },
  "ws.ownerOnly": { en: "Only the workspace owner can delete it. You can remove yourself from the workspace on the Members tab instead.", ms: "Hanya pemilik workspace boleh memadamnya. Anda boleh keluarkan diri anda dari workspace di tab Ahli.", zh: "只有工作区所有者可以删除它。您可以在“成员”标签中将自己移出工作区。", ta: "பணியிடத்தின் உரிமையாளர் மட்டுமே அதை நீக்க முடியும். அதற்குப் பதிலாக \"உறுப்பினர்கள்\" தாவலில் உங்களை நீக்கலாம்.", ar: "لا يمكن حذف مساحة العمل إلا لمالكها. يمكنك إزالة نفسك من مساحة العمل من تبويب الأعضاء بدلاً من ذلك." },
  "ws.lastWorkspace": { en: "This is your only workspace, and it cannot be deleted. You always need at least one. Create another one first.", ms: "Ini satu-satunya workspace anda, jadi ia tidak boleh dipadam — anda sentiasa perlukan sekurang-kurangnya satu. Cipta satu lagi dahulu.", zh: "这是您唯一的工作区，无法删除——您始终需要至少一个。请先创建另一个。", ta: "இது உங்கள் ஒரே பணியிடம், எனவே நீக்க முடியாது — எப்போதும் குறைந்தது ஒன்று தேவை. முதலில் மற்றொன்றை உருவாக்கவும்.", ar: "هذه مساحة عملك الوحيدة، ولا يمكن حذفها — تحتاج دائماً إلى واحدة على الأقل. أنشئ أخرى أولاً." },
  "ws.notEmpty": { en: "This workspace still holds the following. None of it is destroyed by moving the workspace to the Trash:", ms: "Workspace ini masih menyimpan perkara berikut. Tiada satu pun dimusnahkan dengan menghantarnya ke Tong Sampah:", zh: "此工作区仍包含以下内容。将其移入回收站不会销毁其中任何一项：", ta: "இந்தப் பணியிடத்தில் இவை இன்னும் உள்ளன. குப்பைத் தொட்டிக்கு நகர்த்துவதால் இவை எதுவும் அழிக்கப்படாது:", ar: "ما زالت مساحة العمل هذه تحتوي على ما يلي. لا يُتلف أي منه عند نقل مساحة العمل إلى سلة المهملات:" },
  "ws.notEmptyHint": { en: "Destroying it for good is reached through the REST API or an MCP client, not the dashboard. A workspace with sales refuses that, because sales are your record of money received.", ms: "Semuanya kekal di cakera, jadi penghantaran ini boleh dibalikkan. Pemadaman kekal dicapai melalui REST API atau klien MCP, bukan papan pemuka, dan workspace yang mempunyai jualan menolaknya — jualan ialah rekod wang yang anda terima.", zh: "所有内容都保留在磁盘上，因此此操作可撤销。永久删除需通过 REST API 或 MCP 客户端进行，而非面板，而有销售记录的工作区会拒绝该操作——销售是您收到款项的记录。", ta: "அனைத்தும் வட்டில் இருக்கும், எனவே இது மீளக்கூடியது. நிரந்தர நீக்கம் REST API அல்லது MCP கிளையன் வழியாக நடக்கும், டாஷ்போர்டில் இல்லை; விற்பனை உள்ள பணியிடம் அதை மறுக்கும் — விற்பனை என்பது நீங்கள் பெற்ற பணத்தின் பதிவு.", ar: "يبقى كل شيء على القرص، لذا فإن النقل قابل للتراجع. أما الحذف النهائي فيتم عبر REST API أو عميل MCP، لا من لوحة التحكم، ومساحة العمل التي بها مبيعات ترفضه — فالمبيعات سجل الأموال التي استلمتها." },
  // The Danger zone's census labels, keyed by the API's stable `kind` code
  // (card t_088521a4). THE API ALSO SENDS A `label` and that is where the defect
  // was: the label is hardcoded Malay (`blockerLabels` in
  // api/internal/dbstore/workspace.go) and cannot follow the UI locale, so an
  // English dashboard listed its own contents as "borang … / produk … / jualan …
  // / domain tersuai …" under an English sentence. The key is DERIVED from the
  // kind (`ws.blocker.<kind>`), never looked up through a second kind->key table,
  // so there is exactly ONE list to keep — the dictionary a translator edits.
  // `src/lib/workspaceBlockerLabel.test.ts` pins every kind the server can emit
  // against these entries, in both directions, so a new server category cannot
  // ship nameless and a renamed one cannot leave a dead key behind.
  "ws.blocker.forms": { en: "forms (including those in the Trash)", ms: "borang (termasuk yang dalam tong sampah)", zh: "表单（包括回收站中的）", ta: "படிவங்கள் (குப்பைத் தொட்டியில் உள்ளவை உட்பட)", ar: "النماذج (بما في ذلك الموجودة في سلة المهملات)" },
  "ws.blocker.products": { en: "products", ms: "produk", zh: "商品", ta: "தயாரிப்புகள்", ar: "المنتجات" },
  "ws.blocker.sales": { en: "sales", ms: "jualan", zh: "销售", ta: "விற்பனைகள்", ar: "المبيعات" },
  "ws.blocker.subscriptions": { en: "subscriptions (billing history)", ms: "langganan (sejarah bil)", zh: "订阅（账单历史）", ta: "சந்தாக்கள் (பில்லிங் வரலாறு)", ar: "الاشتراكات (سجل الفواتير)" },
  "ws.blocker.domains": { en: "custom domains", ms: "domain tersuai", zh: "自定义域名", ta: "தனிப்பயன் டொமைன்கள்", ar: "النطاقات المخصصة" },
  "ws.blocker.webhooks": { en: "webhooks", ms: "webhook", zh: "Webhook", ta: "வெப்ஹூக்குகள்", ar: "خطافات الويب" },
  "ws.blocker.api_keys": { en: "API keys", ms: "kunci API", zh: "API 密钥", ta: "API விசைகள்", ar: "مفاتيح API" },
  "ws.deleteButton": { en: "Move this workspace to the Trash", ms: "Hantar workspace ini ke Tong Sampah", zh: "将此工作区移入回收站", ta: "இந்தப் பணியிடத்தைக் குப்பைத் தொட்டிக்கு நகர்த்து", ar: "نقل مساحة العمل هذه إلى سلة المهملات" },
  "ws.deleteHint": { en: "Nothing is destroyed. You can restore it from the Trash at any time.", ms: "Tiada apa-apa dimusnahkan — anda boleh memulihkannya dari Tong Sampah bila-bila masa.", zh: "不会销毁任何内容——您可以随时从回收站恢复。", ta: "எதுவும் அழிக்கப்படவில்லை — எப்போது வேண்டுமானாலும் குப்பைத் தொட்டியிலிருந்து மீட்டெடுக்கலாம்.", ar: "لا يُتلف أي شيء — يمكنك استعادته من سلة المهملات في أي وقت." },
  "ws.confirmTitle": { en: "Move this workspace to the Trash?", ms: "Hantar workspace ini ke Tong Sampah?", zh: "将此工作区移入回收站？", ta: "இந்தப் பணியிடத்தைக் குப்பைத் தொட்டிக்கு நகர்த்தவா?", ar: "نقل مساحة العمل هذه إلى سلة المهملات؟" },
  "ws.confirmBody": { en: "Nothing is destroyed. The workspace leaves your workspace list and waits in the Trash with its forms, products, sales and payment account, so you can restore it any time. Deleting it for good is a separate step reached through the REST API or an MCP client acting for the owner, not from this view. Type the address below to confirm.", ms: "Tiada apa-apa dimusnahkan. Workspace ini keluar dari senarai workspace anda dan menunggu dalam Tong Sampah bersama borang, produk, jualan dan akaun pembayarannya — anda boleh memulihkannya bila-bila masa. Pemadaman kekal ialah langkah berasingan yang dicapai melalui REST API atau klien MCP bagi pihak pemilik — bukan dari paparan ini. Taip alamat di bawah untuk sahkan.", zh: "不会销毁任何内容。该工作区将从您的工作区列表中移除，连同其表单、商品、销售及收款账户一起留在回收站——您可以随时恢复。永久删除是单独的步骤，需通过 REST API 或以所有者身份运行的 MCP 客户端执行——不能在此视图中完成。请输入下方地址以确认。", ta: "எதுவும் அழிக்கப்படாது. பணியிடம் உங்கள் பட்டியலிலிருந்து விலகி, அதன் படிவங்கள், தயாரிப்புகள், விற்பனைகள் மற்றும் கட்டணக் கணக்குடன் குப்பைத் தொட்டியில் காத்திருக்கும் — எப்போது வேண்டுமானாலும் மீட்டெடுக்கலாம். நிரந்தர நீக்கம் தனி படி — REST API அல்லது உரிமையாளருக்காக இயங்கும் MCP கிளையன் வழியாக. இந்தப் பார்வையில் இல்லை. உறுதிப்படுத்த கீழே உள்ள முகவரியைத் தட்டச்சு செய்யவும்.", ar: "لا يُتلف أي شيء. تخرج مساحة العمل من قائمتك وتنتظر في سلة المهملات مع نماذجها ومنتجاتها ومبيعاتها وحساب الدفع الخاص بها — ويمكنك استعادتها في أي وقت. الحذف النهائي خطوة منفصلة تتم عبر REST API أو عميل MCP نيابة عن المالك — وليس من هذا العرض. اكتب العنوان أدناه للتأكيد." },
  "ws.confirmTypeLabel": { en: "Address to type:", ms: "Alamat untuk ditaip:", zh: "需输入的地址：", ta: "தட்டச்சு செய்ய வேண்டிய முகவரி:", ar: "العنوان المطلوب كتابته:" },
  "ws.confirmFieldLabel": { en: "Workspace address", ms: "Alamat workspace", zh: "工作区地址", ta: "பணியிட முகவரி", ar: "عنوان مساحة العمل" },
  "ws.confirmButton": { en: "Move to Trash", ms: "Hantar ke Tong Sampah", zh: "移入回收站", ta: "குப்பைத் தொட்டிக்கு நகர்த்து", ar: "النقل إلى سلة المهملات" },
  "ws.trashTitle": { en: "Trash", ms: "Tong Sampah", zh: "回收站", ta: "குப்பைத் தொட்டி", ar: "سلة المهملات" },
  "ws.trashEmpty": { en: "The Trash is empty.", ms: "Tong Sampah kosong.", zh: "回收站是空的。", ta: "குப்பைத் தொட்டி காலியாக உள்ளது.", ar: "سلة المهملات فارغة." },
  "ws.trashHelp": { en: "Workspaces you moved to the Trash. Restore one to bring it back with everything it held. Nothing was destroyed.", ms: "Workspace yang anda hantar ke Tong Sampah. Pulihkan satu untuk mengembalikannya bersama semua kandungannya — tiada apa-apa dimusnahkan.", zh: "您移入回收站的工作区。恢复其中一个即可将其连同所有内容一并取回——没有任何内容被销毁。", ta: "நீங்கள் குப்பைத் தொட்டிக்கு நகர்த்திய பணியிடங்கள். ஒன்றை மீட்டெடுத்தால் அது அனைத்துடன் திரும்பும் — எதுவும் அழிக்கப்படவில்லை.", ar: "مساحات العمل التي نقلتها إلى سلة المهملات. استعد إحداها لتعود بكل ما كانت تحتويه — لم يُتلف أي شيء." },
  "ws.trashDeletedAt": { en: "Moved to Trash", ms: "Dihantar ke Tong Sampah", zh: "已移入回收站", ta: "குப்பைத் தொட்டிக்கு நகர்த்தப்பட்டது", ar: "نُقلت إلى سلة المهملات" },
  // {count} is the row's sales count. Says why a permanent delete is refused
  // rather than offering a button the server will reject: sales are the
  // merchant's record of money received and are never destroyed with a workspace.
  "ws.trashHeldBySales": { en: "Held by sales: {count}. These cannot be destroyed with the workspace.", ms: "Terikat pada jualan: {count} — ini tidak boleh dimusnahkan bersama workspace.", zh: "受销售记录约束：{count} 条——这些不能随工作区一起销毁。", ta: "விற்பனைகளால் பிணைக்கப்பட்டுள்ளது: {count} — இவை பணியிடத்துடன் அழிக்கப்பட முடியாது.", ar: "مرتبطة بمبيعات: {count} — لا يمكن إتلافها مع مساحة العمل." },
  "ws.trashRestore": { en: "Restore", ms: "Pulihkan", zh: "恢复", ta: "மீட்டெடு", ar: "استعادة" },
  "ws.trashRestoring": { en: "Restoring…", ms: "Memulihkan…", zh: "正在恢复…", ta: "மீட்டெடுக்கிறது…", ar: "جارٍ الاستعادة…" },
  "ws.trashLoadFail": { en: "We couldn't load the Trash. Please try again.", ms: "Kami tidak dapat memuatkan Tong Sampah. Sila cuba lagi.", zh: "无法加载回收站。请重试。", ta: "குப்பைத் தொட்டியை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل سلة المهملات. يرجى المحاولة مرة أخرى." },
  "ws.trashRestoreFail": { en: "We couldn't restore this workspace. Please try again.", ms: "Kami tidak dapat memulihkan workspace ini. Sila cuba lagi.", zh: "无法恢复此工作区。请重试。", ta: "இந்தப் பணியிடத்தை மீட்டெடுக்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر استعادة مساحة العمل هذه. يرجى المحاولة مرة أخرى." },
  "ws.loadFail": { en: "We couldn't check this workspace. Please try again.", ms: "Kami tidak dapat menyemak workspace ini. Sila cuba lagi.", zh: "无法检查此工作区。请重试。", ta: "இந்தப் பணியிடத்தைச் சரிபார்க்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر فحص مساحة العمل هذه. يرجى المحاولة مرة أخرى." },
  "ws.deleteFail": { en: "We couldn't delete this workspace. Please try again.", ms: "Kami tidak dapat memadam workspace ini. Sila cuba lagi.", zh: "无法删除此工作区。请重试。", ta: "இந்தப் பணியிடத்தை நீக்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر حذف مساحة العمل هذه. يرجى المحاولة مرة أخرى." },
  "ws.docs": { en: "How deleting a workspace works", ms: "Cara memadam workspace", zh: "删除工作区的工作方式", ta: "பணியிடத்தை நீக்குவது எப்படி நடக்கிறது", ar: "كيف يعمل حذف مساحة العمل" },
  // Forms page: the Trash view. A trashed form is hidden from the normal list
  // but still holds its row — and still counts against a workspace delete —
  // until it is restored or purged for good.
  "forms.trashTab": { en: "Active", ms: "Aktif", zh: "使用中", ta: "செயலில்", ar: "النشطة" },
  "forms.trashTabDeleted": { en: "Trash", ms: "Tong Sampah", zh: "回收站", ta: "குப்பைத் தொட்டி", ar: "سلة المهملات" },
  "forms.trashEmpty": { en: "The trash is empty.", ms: "Tong sampah kosong.", zh: "回收站为空。", ta: "குப்பைத் தொட்டி காலியாக உள்ளது.", ar: "سلة المهملات فارغة." },
  "forms.trashNote": { en: "Deleted forms stay here until you delete them for good. They still belong to this workspace, so a workspace cannot be deleted while its trash is not empty.", ms: "Borang yang dipadam kekal di sini sehingga anda padam kekalnya. Ia masih milik workspace ini, jadi workspace tidak boleh dipadam selagi tong sampahnya tidak kosong.", zh: "已删除的表单会留在这里，直到您永久删除它们。它们仍属于此工作区，因此回收站未清空时无法删除工作区。", ta: "நீக்கப்பட்ட படிவங்கள் நிரந்தரமாக நீக்கும் வரை இங்கே இருக்கும். அவை இந்தப் பணியிடத்திற்கே சொந்தம், எனவே குப்பைத் தொட்டி காலியாக இல்லாதவரை பணியிடத்தை நீக்க முடியாது.", ar: "تبقى النماذج المحذوفة هنا حتى تحذفها نهائياً. لا تزال تنتمي إلى مساحة العمل هذه، لذا لا يمكن حذف مساحة العمل ما دامت سلة مهملاتها غير فارغة." },
  "forms.trashDeletedAt": { en: "Deleted", ms: "Dipadam", zh: "已删除", ta: "நீக்கப்பட்டது", ar: "حُذف" },
  "forms.restore": { en: "Restore", ms: "Pulihkan", zh: "恢复", ta: "மீட்டமை", ar: "استعادة" },
  "forms.restored": { en: "Form restored. It is back in your list.", ms: "Borang dipulihkan — ia kembali dalam senarai anda.", zh: "表单已恢复——已回到您的列表中。", ta: "படிவம் மீட்டமைக்கப்பட்டது — உங்கள் பட்டியலில் திரும்பியது.", ar: "تمت استعادة النموذج — عاد إلى قائمتك." },
  "forms.restoreFail": { en: "We couldn't restore this form. Please try again.", ms: "Kami tidak dapat memulihkan borang ini. Sila cuba lagi.", zh: "无法恢复此表单。请重试。", ta: "இந்தப் படிவத்தை மீட்டமைக்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّرت استعادة هذا النموذج. يرجى المحاولة مرة أخرى." },
  "forms.restoreSlugTaken": { en: "Another form has taken this address, so this one cannot be restored. Rename the other form first, or delete this one for good.", ms: "Borang lain telah mengambil alamat ini, jadi borang ini tidak boleh dipulihkan. Namakan semula borang yang satu lagi dahulu, atau padam kekal borang ini.", zh: "另一个表单已占用此地址，因此无法恢复此表单。请先重命名另一个表单，或永久删除此表单。", ta: "வேறொரு படிவம் இந்த முகவரியை எடுத்துக்கொண்டது, எனவே இதை மீட்டமைக்க முடியாது. முதலில் மற்றப் படிவத்தின் பெயரை மாற்றவும், அல்லது இதை நிரந்தரமாக நீக்கவும்.", ar: "استولى نموذج آخر على هذا العنوان، لذا لا يمكن استعادة هذا النموذج. أعد تسمية النموذج الآخر أولاً، أو احذف هذا نهائياً." },
  "forms.purge": { en: "Delete for good", ms: "Padam kekal", zh: "永久删除", ta: "நிரந்தரமாக நீக்கு", ar: "حذف نهائي" },
  "forms.purgeConfirmTitle": { en: "Delete this form for good?", ms: "Padam borang ini secara kekal?", zh: "永久删除此表单？", ta: "இந்தப் படிவத்தை நிரந்தரமாக நீக்கவா?", ar: "حذف هذا النموذج نهائياً؟" },
  "forms.purgeConfirmBody": { en: "This cannot be undone. The form and its responses, notifications and webhooks are removed. Type the form address to confirm.", ms: "Ini tidak boleh dibatalkan — borang serta jawapannya, notifikasi dan webhook akan dibuang. Taip alamat borang untuk sahkan.", zh: "此操作无法撤销——表单及其回复、通知和 Webhook 都将被移除。请输入表单地址以确认。", ta: "இதை மீளச் செய்ய முடியாது — படிவமும் அதன் பதில்கள், அறிவிப்புகள், வெப்ஹூக்குகள் நீக்கப்படும். உறுதிப்படுத்த படிவ முகவரியைத் தட்டச்சு செய்யவும்.", ar: "لا يمكن التراجع عن هذا — سيُزال النموذج وردوده وإشعاراته وخطافات الويب الخاصة به. اكتب عنوان النموذج للتأكيد." },
  "forms.purgeConfirmFieldLabel": { en: "Form address", ms: "Alamat borang", zh: "表单地址", ta: "படிவ முகவரி", ar: "عنوان النموذج" },
  "forms.purged": { en: "Form deleted for good.", ms: "Borang dipadam secara kekal.", zh: "表单已永久删除。", ta: "படிவம் நிரந்தரமாக நீக்கப்பட்டது.", ar: "تم حذف النموذج نهائياً." },
  "forms.purgeFail": { en: "We couldn't delete this form. Please try again.", ms: "Kami tidak dapat memadam borang ini. Sila cuba lagi.", zh: "无法删除此表单。请重试。", ta: "இந்தப் படிவத்தை நீக்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر حذف هذا النموذج. يرجى المحاولة مرة أخرى." },
  "forms.purgeHasSales": { en: "This form has sales, so it cannot be deleted for good. Sales history is never destroyed. It stays in the trash.", ms: "Borang ini mempunyai jualan, jadi ia tidak boleh dipadam kekal — sejarah jualan tidak pernah dimusnahkan. Ia kekal dalam tong sampah.", zh: "此表单已有销售记录，因此无法永久删除——销售记录绝不会被销毁。它将留在回收站中。", ta: "இந்தப் படிவத்தில் விற்பனைகள் உள்ளன, எனவே நிரந்தரமாக நீக்க முடியாது — விற்பனை வரலாறு ஒருபோதும் அழிக்கப்படாது. இது குப்பைத் தொட்டியில் இருக்கும்.", ar: "يحتوي هذا النموذج على مبيعات، لذا لا يمكن حذفه نهائياً — لا يُتلف سجل المبيعات أبداً. سيبقى في سلة المهملات." },
  "forms.canPurgeFalse": { en: "Has sales. Cannot be deleted for good", ms: "Ada jualan — tidak boleh dipadam kekal", zh: "有销售记录——无法永久删除", ta: "விற்பனைகள் உள்ளன — நிரந்தரமாக நீக்க முடியாது", ar: "يحتوي على مبيعات — لا يمكن حذفه نهائياً" },
  "forms.loadTrashFail": { en: "We couldn't load the trash. Please try again.", ms: "Kami tidak dapat memuatkan tong sampah. Sila cuba lagi.", zh: "无法加载回收站。请重试。", ta: "குப்பைத் தொட்டியை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل سلة المهملات. يرجى المحاولة مرة أخرى." },

  // ── Products trash (Tong Sampah) ──────────────────────────────────────────
  // The same three verbs the forms bin uses, on the product bin. The wording is
  // deliberately parallel so a merchant learns ONE model across the three pages.
  "prod.trashTab": { en: "Active", ms: "Aktif", zh: "使用中", ta: "செயலில்", ar: "النشطة" },
  "prod.trashTabDeleted": { en: "Trash", ms: "Tong Sampah", zh: "回收站", ta: "குப்பைத் தொட்டி", ar: "سلة المهملات" },
  "prod.trashEmpty": { en: "The trash is empty.", ms: "Tong sampah kosong.", zh: "回收站为空。", ta: "குப்பைத் தொட்டி காலியாக உள்ளது.", ar: "سلة المهملات فارغة." },
  "prod.trashNote": { en: "Deleted products stay here until you delete them for good. A product here is no longer in your catalogue, which is different from an archived product: that one is still listed but hidden from buyers.", ms: "Produk yang dipadam kekal di sini sehingga anda padam kekalnya. Produk di sini sudah tiada dalam katalog anda — ia berbeza daripada produk yang diarkib, yang masih tersenarai tetapi disorok dari pembeli.", zh: "已删除的商品会留在这里，直到您永久删除它们。这里的商品已不在您的商品目录中——这与已归档的商品不同，后者仍然列出但对买家隐藏。", ta: "நீக்கப்பட்ட தயாரிப்புகள் நிரந்தரமாக நீக்கும் வரை இங்கே இருக்கும். இங்குள்ள தயாரிப்பு உங்கள் பட்டியலில் இல்லை — இது காப்பகப்படுத்தப்பட்ட தயாரிப்பிலிருந்து வேறுபடும்; அது பட்டியலில் இருக்கும் ஆனால் வாங்குபவர்களுக்கு மறைக்கப்படும்.", ar: "تبقى المنتجات المحذوفة هنا حتى تحذفها نهائياً. المنتج هنا لم يعد ضمن كتالوجك — وهذا يختلف عن المنتج المؤرشف، الذي يبقى مُدرجاً لكنه مخفي عن المشترين." },
  "prod.restore": { en: "Restore", ms: "Pulihkan", zh: "恢复", ta: "மீட்டமை", ar: "استعادة" },
  "prod.restored": { en: "Product restored. It is back in your catalogue.", ms: "Produk dipulihkan — ia kembali dalam katalog anda.", zh: "商品已恢复——已回到您的目录中。", ta: "தயாரிப்பு மீட்டமைக்கப்பட்டது — உங்கள் பட்டியலில் திரும்பியது.", ar: "تمت استعادة المنتج — عاد إلى كتالوجك." },
  "prod.restoreFail": { en: "We couldn't restore this product. Please try again.", ms: "Kami tidak dapat memulihkan produk ini. Sila cuba lagi.", zh: "无法恢复此商品。请重试。", ta: "இந்தத் தயாரிப்பை மீட்டமைக்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّرت استعادة هذا المنتج. يرجى المحاولة مرة أخرى." },
  "prod.purge": { en: "Delete for good", ms: "Padam kekal", zh: "永久删除", ta: "நிரந்தரமாக நீக்கு", ar: "حذف نهائي" },
  "prod.purgeConfirmTitle": { en: "Delete this product for good?", ms: "Padam produk ini secara kekal?", zh: "永久删除此商品？", ta: "இந்தத் தயாரிப்பை நிரந்தரமாக நீக்கவா?", ar: "حذف هذا المنتج نهائياً؟" },
  "prod.purgeConfirmBody": { en: "This cannot be undone. The product and all of its variations are removed. Type the product name to confirm.", ms: "Ini tidak boleh dibatalkan — produk dan semua variasinya akan dibuang. Taip nama produk untuk sahkan.", zh: "此操作无法撤销——商品及其所有规格都将被移除。请输入商品名称以确认。", ta: "இதை மீளச் செய்ய முடியாது — தயாரிப்பும் அதன் அனைத்து மாறுபாடுகளும் நீக்கப்படும். உறுதிப்படுத்த தயாரிப்பின் பெயரைத் தட்டச்சு செய்யவும்.", ar: "لا يمكن التراجع عن هذا — سيُزال المنتج وكل تنويعاته. اكتب اسم المنتج للتأكيد." },
  "prod.purgeConfirmFieldLabel": { en: "Product name", ms: "Nama produk", zh: "商品名称", ta: "தயாரிப்பு பெயர்", ar: "اسم المنتج" },
  "prod.purged": { en: "Product deleted for good.", ms: "Produk dipadam secara kekal.", zh: "商品已永久删除。", ta: "தயாரிப்பு நிரந்தரமாக நீக்கப்பட்டது.", ar: "تم حذف المنتج نهائياً." },
  "prod.purgeFail": { en: "We couldn't delete this product. Please try again.", ms: "Kami tidak dapat memadam produk ini. Sila cuba lagi.", zh: "无法删除此商品。请重试。", ta: "இந்தத் தயாரிப்பை நீக்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر حذف هذا المنتج. يرجى المحاولة مرة أخرى." },
  "prod.purgeHasSales": { en: "This product has sales, so it cannot be deleted for good. Sales history is never destroyed. It stays in the trash.", ms: "Produk ini mempunyai jualan, jadi ia tidak boleh dipadam kekal — sejarah jualan tidak pernah dimusnahkan. Ia kekal dalam tong sampah.", zh: "此商品已有销售记录，因此无法永久删除——销售记录绝不会被销毁。它将留在回收站中。", ta: "இந்தத் தயாரிப்பில் விற்பனைகள் உள்ளன, எனவே நிரந்தரமாக நீக்க முடியாது — விற்பனை வரலாறு ஒருபோதும் அழிக்கப்படாது. இது குப்பைத் தொட்டியில் இருக்கும்.", ar: "يحتوي هذا المنتج على مبيعات، لذا لا يمكن حذفه نهائياً — لا يُتلف سجل المبيعات أبداً. سيبقى في سلة المهملات." },
  "prod.purgeHasSubscriptions": { en: "This product still has subscriptions, so it cannot be deleted for good. Cancel them first. It stays in the trash.", ms: "Produk ini masih ada langganan, jadi ia tidak boleh dipadam kekal — batalkan langganan itu dahulu. Ia kekal dalam tong sampah.", zh: "此商品仍有订阅，因此无法永久删除——请先取消这些订阅。它将留在回收站中。", ta: "இந்தத் தயாரிப்பில் இன்னும் சந்தாக்கள் உள்ளன, எனவே நிரந்தரமாக நீக்க முடியாது — முதலில் அவற்றை ரத்து செய்யவும். இது குப்பைத் தொட்டியில் இருக்கும்.", ar: "لا تزال لهذا المنتج اشتراكات، لذا لا يمكن حذفه نهائياً — ألغِها أولاً. سيبقى في سلة المهملات." },
  "prod.canPurgeFalse": { en: "Cannot be deleted for good:", ms: "Tidak boleh dipadam kekal:", zh: "无法永久删除：", ta: "நிரந்தரமாக நீக்க முடியாது:", ar: "لا يمكن حذفه نهائياً:" },
  "prod.loadTrashFail": { en: "We couldn't load the trash. Please try again.", ms: "Kami tidak dapat memuatkan tong sampah. Sila cuba lagi.", zh: "无法加载回收站。请重试。", ta: "குப்பைத் தொட்டியை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل سلة المهملات. يرجى المحاولة مرة أخرى." },
  "prod.deletedToTrash": { en: "Product moved to the trash.", ms: "Produk dipindahkan ke tong sampah.", zh: "商品已移入回收站。", ta: "தயாரிப்பு குப்பைத் தொட்டிக்கு நகர்த்தப்பட்டது.", ar: "نُقل المنتج إلى سلة المهملات." },
  "prod.trashDeletedAt": { en: "Deleted", ms: "Dipadam", zh: "已删除", ta: "நீக்கப்பட்டது", ar: "حُذف" },
  "prod.salesCount": { en: "sales", ms: "jualan", zh: "笔销售", ta: "விற்பனைகள்", ar: "مبيعات" },
  "prod.subsCount": { en: "subscriptions", ms: "langganan", zh: "个订阅", ta: "சந்தாக்கள்", ar: "اشتراكات" },

  // ── Media trash (Tong Sampah) ─────────────────────────────────────────────
  "media.trashTab": { en: "Active", ms: "Aktif", zh: "使用中", ta: "செயலில்", ar: "النشطة" },
  "media.trashTabDeleted": { en: "Trash", ms: "Tong Sampah", zh: "回收站", ta: "குப்பைத் தொட்டி", ar: "سلة المهملات" },
  "media.trashEmpty": { en: "The trash is empty.", ms: "Tong sampah kosong.", zh: "回收站为空。", ta: "குப்பைத் தொட்டி காலியாக உள்ளது.", ar: "سلة المهملات فارغة." },
  "media.trashNote": { en: "Deleted files stay here until you delete them for good. They still count against your storage limit, because the file is still stored. Delete for good to free the space.", ms: "Fail yang dipadam kekal di sini sehingga anda padam kekalnya. Ia masih dikira dalam had storan anda kerana fail itu masih disimpan — padam kekal untuk melepaskan ruang.", zh: "已删除的文件会留在这里，直到您永久删除它们。它们仍计入您的存储上限，因为文件仍被保存——永久删除即可释放空间。", ta: "நீக்கப்பட்ட கோப்புகள் நிரந்தரமாக நீக்கும் வரை இங்கே இருக்கும். கோப்பு இன்னும் சேமிக்கப்பட்டிருப்பதால் அவை உங்கள் சேமிப்பு வரம்பில் கணக்கிடப்படும் — இடத்தை விடுவிக்க நிரந்தரமாக நீக்கவும்.", ar: "تبقى الملفات المحذوفة هنا حتى تحذفها نهائياً. لا تزال تُحتسب ضمن حد التخزين لديك لأن الملف ما زال مخزَّناً — احذفه نهائياً لتحرير المساحة." },
  "media.trashDeletedAt": { en: "Deleted", ms: "Dipadam", zh: "已删除", ta: "நீக்கப்பட்டது", ar: "حُذف" },
  "media.restore": { en: "Restore", ms: "Pulihkan", zh: "恢复", ta: "மீட்டமை", ar: "استعادة" },
  "media.restored": { en: "File restored. It is back in your library.", ms: "Fail dipulihkan — ia kembali dalam pustaka anda.", zh: "文件已恢复——已回到您的媒体库中。", ta: "கோப்பு மீட்டமைக்கப்பட்டது — உங்கள் நூலகத்தில் திரும்பியது.", ar: "تمت استعادة الملف — عاد إلى مكتبتك." },
  "media.restoreFail": { en: "We couldn't restore this file. Please try again.", ms: "Kami tidak dapat memulihkan fail ini. Sila cuba lagi.", zh: "无法恢复此文件。请重试。", ta: "இந்தக் கோப்பை மீட்டமைக்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّرت استعادة هذا الملف. يرجى المحاولة مرة أخرى." },
  "media.restoreObjectGone": { en: "The file itself is no longer in storage, so this item cannot be restored.", ms: "Fail itu sendiri sudah tiada dalam storan, jadi item ini tidak boleh dipulihkan.", zh: "文件本身已不在存储中，因此无法恢复此项。", ta: "கோப்பு இனி சேமிப்பில் இல்லை, எனவே இதை மீட்டமைக்க முடியாது.", ar: "الملف نفسه لم يعد في التخزين، لذا لا يمكن استعادة هذا العنصر." },
  "media.purge": { en: "Delete for good", ms: "Padam kekal", zh: "永久删除", ta: "நிரந்தரமாக நீக்கு", ar: "حذف نهائي" },
  "media.purgeConfirmTitle": { en: "Delete this file for good?", ms: "Padam fail ini secara kekal?", zh: "永久删除此文件？", ta: "இந்தக் கோப்பை நிரந்தரமாக நீக்கவா?", ar: "حذف هذا الملف نهائياً؟" },
  "media.purgeConfirmBody": { en: "This cannot be undone. The file is removed from storage and the space is released. A copy may stay in the CDN cache for a few minutes.", ms: "Ini tidak boleh dibatalkan — fail dibuang dari storan dan ruang dilepaskan. Salinan mungkin masih tersimpan dalam cache CDN selama beberapa minit.", zh: "此操作无法撤销——文件将从存储中移除并释放空间。副本可能在 CDN 缓存中保留几分钟。", ta: "இதை மீளச் செய்ய முடியாது — கோப்பு சேமிப்பிலிருந்து நீக்கப்பட்டு இடம் விடுவிக்கப்படும். ஒரு நகல் சில நிமிடங்களுக்கு CDN தற்காலிக சேமிப்பில் இருக்கக்கூடும்.", ar: "لا يمكن التراجع عن هذا — سيُزال الملف من التخزين وتُحرَّر المساحة. قد تبقى نسخة في ذاكرة CDN المؤقتة لبضع دقائق." },
  "media.purged": { en: "File deleted for good.", ms: "Fail dipadam secara kekal.", zh: "文件已永久删除。", ta: "கோப்பு நிரந்தரமாக நீக்கப்பட்டது.", ar: "تم حذف الملف نهائياً." },
  // t_cb4e3d99: the purge response names the edge outcome, and these two keys
  // are what the toast says when the CDN copy was NOT confirmed cleared. The
  // plain "deleted for good" line above is only honest for `edge: purged`.
  "media.purgedEdgeStale": { en: "File removed from storage. The space is released. The CDN may still serve its cached copy for up to {seconds} seconds.", ms: "Fail dibuang dari storan — ruang dilepaskan. CDN mungkin masih menyajikan salinan cache sehingga {seconds} saat.", zh: "文件已从存储中移除——空间已释放。CDN 可能仍会继续提供其缓存副本，最长 {seconds} 秒。", ta: "கோப்பு சேமிப்பிலிருந்து நீக்கப்பட்டது — இடம் விடுவிக்கப்பட்டது. CDN அதன் தற்காலிக நகலை இன்னும் {seconds} வினாடிகள் வரை வழங்கக்கூடும்.", ar: "أُزيل الملف من التخزين — وحُرِّرت المساحة. قد تواصل شبكة CDN تقديم نسختها المخزَّنة مؤقتاً لمدة تصل إلى {seconds} ثانية." },
  "media.purgedEdgeUnnamed": { en: "File removed from storage. The space is released. We could not confirm the CDN copy was cleared, so a cached copy may still be served for a short while.", ms: "Fail dibuang dari storan — ruang dilepaskan. Kami tidak dapat mengesahkan salinan CDN telah dibersihkan, jadi salinan cache mungkin masih disajikan untuk seketika.", zh: "文件已从存储中移除——空间已释放。我们无法确认 CDN 副本已被清除，因此缓存副本可能仍会被提供一小段时间。", ta: "கோப்பு சேமிப்பிலிருந்து நீக்கப்பட்டது — இடம் விடுவிக்கப்பட்டது. CDN நகல் அழிக்கப்பட்டதை உறுதிப்படுத்த முடியவில்லை, எனவே தற்காலிக நகல் சிறிது நேரம் வழங்கப்படக்கூடும்.", ar: "أُزيل الملف من التخزين — وحُرِّرت المساحة. تعذّر التأكد من مسح نسخة CDN، لذا قد تظل النسخة المخزَّنة مؤقتاً تُقدَّم لفترة قصيرة." },
  "media.purgeFail": { en: "We couldn't delete this file. Please try again.", ms: "Kami tidak dapat memadam fail ini. Sila cuba lagi.", zh: "无法删除此文件。请重试。", ta: "இந்தக் கோப்பை நீக்க முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر حذف هذا الملف. يرجى المحاولة مرة أخرى." },
  "media.loadTrashFail": { en: "We couldn't load the trash. Please try again.", ms: "Kami tidak dapat memuatkan tong sampah. Sila cuba lagi.", zh: "无法加载回收站。请重试。", ta: "குப்பைத் தொட்டியை ஏற்ற முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّر تحميل سلة المهملات. يرجى المحاولة مرة أخرى." },
  "media.deletedToTrash": { en: "File moved to the trash. Restore it from there if you change your mind.", ms: "Fail dipindahkan ke tong sampah — pulihkan dari sana jika anda berubah fikiran.", zh: "文件已移入回收站——如需恢复请到回收站操作。", ta: "கோப்பு குப்பைத் தொட்டிக்கு நகர்த்தப்பட்டது — மனம் மாறினால் அங்கிருந்து மீட்டமைக்கவும்.", ar: "نُقل الملف إلى سلة المهملات — استعده من هناك إن غيّرت رأيك." },
  // t_bac29e18: the /docs quick link to the REST API reference. Every docs quick
  // link has a key here, so the label renders in all five languages.
  "docs.linkAPI": { en: "REST API reference", ms: "Rujukan API REST", zh: "REST API 参考", ta: "REST API குறிப்பு", ar: "مرجع واجهة REST API" },
  "afb.notifyFailedTitle": { en: "Notification not delivered", ms: "Notifikasi tidak dihantar", zh: "通知未送达", ta: "அறிவிப்பு அனுப்பப்படவில்லை", ar: "لم يتم تسليم الإشعار" },
  "afb.notifyFailedBadge": { en: "Email failed", ms: "Emel gagal", zh: "邮件失败", ta: "மின்னஞ்சல் தோல்வி", ar: "فشل البريد" },
  "afb.notifyFailedWho": { en: "The {who} was not told about this thread. The email could not be handed to the delivery queue.", ms: "{who} tidak dimaklumkan tentang thread ini — emel tidak sempat masuk baris gilir penghantaran.", zh: "{who}未收到此对话的通知——邮件未能进入投递队列。", ta: "இந்த உரையாடல் குறித்து {who} க்கு தெரிவிக்கப்படவில்லை — மின்னஞ்சலை விநியோக வரிசைக்கு அனுப்ப முடியவில்லை.", ar: "لم يتم إبلاغ {who} بهذه المحادثة — تعذّر إيداع البريد في قائمة التسليم." },
  "afb.notifyFailedFiler": { en: "filer", ms: "penghantar", zh: "提交者", ta: "சமர்ப்பித்தவர்", ar: "المُرسِل" },
  "afb.notifyFailedAdmin": { en: "JomForm team", ms: "pasukan JomForm", zh: "JomForm 团队", ta: "JomForm குழு", ar: "فريق JomForm" },
  "afb.notifyRetry": { en: "Send the notification again", ms: "Hantar notifikasi semula", zh: "重新发送通知", ta: "அறிவிப்பை மீண்டும் அனுப்பு", ar: "إعادة إرسال الإشعار" },
  "afb.notifyRetrying": { en: "Sending…", ms: "Menghantar…", zh: "正在发送…", ta: "அனுப்புகிறது…", ar: "جارٍ الإرسال…" },
  "afb.notifyRetryDone": { en: "Notification sent again.", ms: "Notifikasi dihantar semula.", zh: "通知已重新发送。", ta: "அறிவிப்பு மீண்டும் அனுப்பப்பட்டது.", ar: "تمت إعادة إرسال الإشعار." },
  "afb.notifyRetryFail": { en: "We couldn't re-send the notification. Please try again.", ms: "Kami tidak dapat menghantar semula notifikasi itu. Sila cuba lagi.", zh: "无法重新发送通知。请重试。", ta: "அறிவிப்பை மீண்டும் அனுப்ப முடியவில்லை. மீண்டும் முயற்சிக்கவும்.", ar: "تعذّرت إعادة إرسال الإشعار. يرجى المحاولة مرة أخرى." },
  "afb.notifyRetrySkipped": { en: "Nothing was sent. There is no recipient configured for this notification, so no email was queued. Ask a platform admin to check the super-admin allowlist.", ms: "Tiada apa-apa dihantar — tiada penerima dikonfigurasikan untuk notifikasi ini, jadi tiada emel dimasukkan ke baris gilir. Minta pentadbir platform menyemak senarai super-admin.", zh: "未发送任何内容——此通知未配置收件人，因此没有邮件进入队列。请让平台管理员检查超级管理员名单。", ta: "எதுவும் அனுப்பப்படவில்லை — இந்த அறிவிப்புக்கு பெறுநர் யாரும் அமைக்கப்படவில்லை, எனவே எந்த மின்னஞ்சலும் வரிசையில் சேர்க்கப்படவில்லை. தள நிர்வாகி சூப்பர்-அட்மின் பட்டியலைச் சரிபார்க்கச் சொல்லுங்கள்.", ar: "لم يُرسل شيء — لا يوجد مستلم مُهيّأ لهذا الإشعار، لذا لم يُدرج أي بريد في قائمة الانتظار. اطلب من مسؤول المنصة التحقق من قائمة المشرفين الأعلى." },
  "afb.notifyRetryStillMarked": { en: "Re-sent, but the thread still records a failure, so it was not delivered to everyone. Check the banner below.", ms: "Dihantar semula — tetapi thread masih merekodkan kegagalan, jadi ia tidak sampai kepada semua pihak. Semak sepanduk di bawah.", zh: "已重新发送——但该对话仍记录有失败，因此并未送达所有人。请查看下方横幅。", ta: "மீண்டும் அனுப்பப்பட்டது — ஆனால் உரையாடலில் இன்னும் தோல்வி பதிவாகியுள்ளது, எனவே அனைவருக்கும் அது செல்லவில்லை. கீழே உள்ள பட்டியைப் பார்க்கவும்.", ar: "أُعيد الإرسال — لكن المحادثة لا تزال تسجّل فشلاً، لذا لم يصل الجميع. تحقق من الشريط أدناه." },
  // Alt text on the public brand-assets page (card t_b432c9ca). The page's
  // prose is English by design; the two images are announced through the
  // viewer's own language setting, which is what alt text is for.
  "brand.logoAlt": { en: "JomForm logo", ms: "Logo JomForm", zh: "JomForm 标志", ta: "JomForm சின்னம்", ar: "شعار JomForm" },
  "brand.bannerAlt": { en: "JomForm banner", ms: "Sepanduk JomForm", zh: "JomForm 横幅", ta: "JomForm பேனர்", ar: "لافتة JomForm" },
  "afb.notifyRetryUnconfirmed": { en: "We could not confirm whether the notification was re-sent. Check the banner below before assuming the filer was told.", ms: "Kami tidak dapat mengesahkan sama ada notifikasi itu dihantar semula — semak sepanduk di bawah sebelum menganggap penghantar telah dimaklumkan.", zh: "无法确认通知是否已重新发送——在假定提交者已获通知前，请查看下方横幅。", ta: "அறிவிப்பு மீண்டும் அனுப்பப்பட்டதா என்பதை உறுதிப்படுத்த முடியவில்லை — சமர்ப்பித்தவர் அறிவிக்கப்பட்டதாகக் கருதும் முன் கீழே உள்ள பட்டியைப் பாருங்கள்.", ar: "تعذّر تأكيد ما إذا أُعيد إرسال الإشعار — تحقق من الشريط أدناه قبل افتراض أن المُرسِل قد أُبلغ." },
  "afb.repliedNotifyFailed": { en: "Reply saved, but the filer could NOT be notified. Use the banner above to send it again.", ms: "Balasan disimpan — tetapi penghantar TIDAK dapat dimaklumkan. Guna sepanduk di atas untuk menghantarnya semula.", zh: "回复已保存——但未能通知提交者。请使用上方横幅重新发送。", ta: "பதில் சேமிக்கப்பட்டது — ஆனால் சமர்ப்பித்தவருக்கு அறிவிக்க முடியவில்லை. மீண்டும் அனுப்ப மேலே உள்ள பட்டியைப் பயன்படுத்தவும்.", ar: "تم حفظ الرد — لكن تعذّر إبلاغ المُرسِل. استخدم الشريط أعلاه لإعادة الإرسال." },
  "afb.repliedUnconfirmed": { en: "Reply saved. The delivery status of the notification was not confirmed. Check the thread before assuming the filer was told.", ms: "Balasan disimpan. Status penghantaran notifikasi tidak disahkan — semak thread sebelum menganggap penghantar telah dimaklumkan.", zh: "回复已保存。通知的投递状态未确认——在假定提交者已获通知前请检查该对话。", ta: "பதில் சேமிக்கப்பட்டது. அறிவிப்பின் விநியோக நிலை உறுதிப்படுத்தப்படவில்லை — சமர்ப்பித்தவருக்கு தெரிவிக்கப்பட்டதாகக் கருதும் முன் உரையாடலைச் சரிபார்க்கவும்.", ar: "تم حفظ الرد. لم يتم تأكيد حالة تسليم الإشعار — تحقق من المحادثة قبل افتراض أن المُرسِل قد أُبلغ." },
} as const;
