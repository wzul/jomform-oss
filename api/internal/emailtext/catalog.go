// The transactional-email catalog: subject + body for every email JomForm
// sends, in each of the five supported locales.
//
// Every string here is either the ENGLISH source (the documented default
// everywhere in the product) or a translation already shipped elsewhere in the
// repo and reused verbatim, so converting a sender to this dictionary does not
// quietly reword an email a user has already seen:
//
//   - magic-link login / signup: the five locales from
//     httpapi/auth_recovery.go magicLinkEmailText (issue #278).
//   - workspace invite: the five locales from
//     httpapi/rest_workspace_members.go inviteEmailSubject / inviteEmailBody
//     (issue #161).
//   - receipt default: the template previously hardcoded in
//     payments/receipt.go and dbstore/form_email.go (issue #107/#453).
//   - notification defaults: the templates dbstore.EnsureDefault installs
//     (issue #193/#597).
//
// The buyer-facing kinds (receipt, notifications, merchant copy) deliberately
// keep their `{{ref_code}}`-style placeholders: those belong to the
// receipt/notification template engine, which runs after this catalog.
package emailtext

// msg is one locale's rendering of a kind.
type msg struct{ Subject, Body string }

// catalog maps a kind to its per-locale message. Every kind MUST carry all five
// locales — emailtext_test.go fails on a missing or empty one, so a new email
// cannot ship half-translated.
var catalog = map[Kind]map[string]msg{
	// ── platform-user mail (resolved from the recipient's users.lang) ────────

	VerifyEmail: {
		En: {Subject: "Verify your JomForm email",
			Body: "Please verify your email address to activate your JomForm account.\n\n" +
				"Tap the link below (valid for 24 hours):\n\n" +
				"{link}\n\n" +
				"If you did not create a JomForm account, you can ignore this email.\n"},
		Ms: {Subject: "Sahkan emel JomForm anda",
			Body: "Sila sahkan alamat emel anda untuk mengaktifkan akaun JomForm anda.\n\n" +
				"Tap pautan berikut (sah selama 24 jam):\n\n" +
				"{link}\n\n" +
				"Jika anda tidak mendaftar akaun JomForm, anda boleh abaikan emel ini.\n"},
		Zh: {Subject: "验证您的 JomForm 邮箱",
			Body: "请验证您的邮箱地址以激活 JomForm 账户。\n\n" +
				"点击以下链接（有效期 24 小时）：\n\n" +
				"{link}\n\n" +
				"如果您没有创建 JomForm 账户，可以忽略此邮件。\n"},
		Ta: {Subject: "உங்கள் JomForm மின்னஞ்சலைச் சரிபார்க்கவும்",
			Body: "உங்கள் JomForm கணக்கைச் செயல்படுத்த உங்கள் மின்னஞ்சல் முகவரியைச் சரிபார்க்கவும்.\n\n" +
				"கீழே உள்ள இணைப்பைத் தட்டவும் (24 மணி நேரம் செல்லுபடியாகும்):\n\n" +
				"{link}\n\n" +
				"நீங்கள் JomForm கணக்கை உருவாக்கவில்லை என்றால், இந்த மின்னஞ்சலைப் புறக்கணிக்கலாம்.\n"},
		Ar: {Subject: "تحقق من بريدك الإلكتروني في JomForm",
			Body: "يرجى التحقق من عنوان بريدك الإلكتروني لتفعيل حسابك في JomForm.\n\n" +
				"انقر على الرابط التالي (صالح لمدة 24 ساعة):\n\n" +
				"{link}\n\n" +
				"إذا لم تنشئ حساباً في JomForm، يمكنك تجاهل هذا البريد.\n"},
	},

	PasswordReset: {
		En: {Subject: "Reset your JomForm password",
			Body: "You asked to reset your JomForm password.\n\n" +
				"Tap the link below to set a new password (valid for 30 minutes):\n\n" +
				"{link}\n\n" +
				"If you did not request this, you can ignore this email — your password is unchanged.\n"},
		Ms: {Subject: "Set semula kata laluan JomForm anda",
			Body: "Anda telah meminta untuk set semula kata laluan JomForm anda.\n\n" +
				"Tap pautan berikut untuk menetapkan kata laluan baharu (sah selama 30 minit):\n\n" +
				"{link}\n\n" +
				"Jika anda tidak meminta ini, anda boleh abaikan emel ini — kata laluan anda tidak berubah.\n"},
		Zh: {Subject: "重置您的 JomForm 密码",
			Body: "您请求重置 JomForm 密码。\n\n" +
				"点击以下链接设置新密码（有效期 30 分钟）：\n\n" +
				"{link}\n\n" +
				"如果您没有请求此项，可以忽略此邮件 — 您的密码保持不变。\n"},
		Ta: {Subject: "உங்கள் JomForm கடவுச்சொல்லை மீட்டமைக்கவும்",
			Body: "உங்கள் JomForm கடவுச்சொல்லை மீட்டமைக்க நீங்கள் கோரியுள்ளீர்கள்.\n\n" +
				"புதிய கடவுச்சொல்லை அமைக்க கீழே உள்ள இணைப்பைத் தட்டவும் (30 நிமிடங்களுக்குச் செல்லுபடியாகும்):\n\n" +
				"{link}\n\n" +
				"நீங்கள் இதைக் கோரவில்லை என்றால், இந்த மின்னஞ்சலைப் புறக்கணிக்கலாம் — உங்கள் கடவுச்சொல் மாறவில்லை.\n"},
		Ar: {Subject: "إعادة تعيين كلمة مرور JomForm",
			Body: "لقد طلبت إعادة تعيين كلمة مرور JomForm.\n\n" +
				"انقر على الرابط التالي لتعيين كلمة مرور جديدة (صالح لمدة 30 دقيقة):\n\n" +
				"{link}\n\n" +
				"إذا لم تطلب ذلك، يمكنك تجاهل هذا البريد — كلمة مرورك لم تتغير.\n"},
	},

	MagicLinkLogin: {
		En: {Subject: "Your JomForm sign-in link",
			Body: "Sign in to JomForm without a password:\n\n" + "{link}\n\n" +
				"This link is valid for 15 minutes and can only be used once.\n\n" +
				"If you didn't request this, you can ignore this email — your account is safe.\n"},
		Ms: {Subject: "Pautan log masuk JomForm anda",
			Body: "Log masuk tanpa kata laluan ke JomForm anda:\n\n" + "{link}\n\n" +
				"Pautan ini sah selama 15 minit dan hanya boleh digunakan sekali.\n\n" +
				"Jika anda tidak meminta ini, anda boleh abaikan emel ini — akaun anda selamat.\n"},
		Zh: {Subject: "您的 JomForm 登录链接",
			Body: "无需密码登录 JomForm：\n\n" + "{link}\n\n" +
				"此链接有效期为 15 分钟，只能使用一次。\n\n" +
				"如果您没有请求此链接，可以忽略此邮件 — 您的账户是安全的。\n"},
		Ta: {Subject: "உங்கள் JomForm உள்நுழைவு இணைப்பு",
			Body: "கடவுச்சொல் இல்லாமல் JomForm இல் உள்நுழைய:\n\n" + "{link}\n\n" +
				"இந்த இணைப்பு 15 நிமிடங்களுக்கு செல்லுபடியாகும், ஒருமுறை மட்டுமே பயன்படுத்தலாம்.\n\n" +
				"நீங்கள் இதைக் கோரவில்லை என்றால், இந்த மின்னஞ்சலைப் புறக்கணிக்கலாம் — உங்கள் கணக்கு பாதுகாப்பானது.\n"},
		Ar: {Subject: "رابط تسجيل الدخول إلى JomForm",
			Body: "تسجيل الدخول إلى JomForm بدون كلمة مرور:\n\n" + "{link}\n\n" +
				"هذا الرابط صالح لمدة 15 دقيقة ويمكن استخدامه مرة واحدة فقط.\n\n" +
				"إذا لم تطلب هذا، يمكنك تجاهل هذا البريد — حسابك آمن.\n"},
	},

	MagicLinkSignup: {
		En: {Subject: "Your JomForm sign-up link",
			Body: "Sign in to JomForm without a password:\n\n" + "{link}\n\n" +
				"This link is valid for 15 minutes and can only be used once. Clicking it will create your account and sign you in.\n\n" +
				"If you didn't request this, you can ignore this email.\n"},
		Ms: {Subject: "Pautan pendaftaran JomForm anda",
			Body: "Log masuk tanpa kata laluan ke JomForm anda:\n\n" + "{link}\n\n" +
				"Pautan ini sah selama 15 minit dan hanya boleh digunakan sekali. Mengkliknya akan mencipta akaun anda dan log masuk.\n\n" +
				"Jika anda tidak meminta ini, anda boleh abaikan emel ini.\n"},
		Zh: {Subject: "您的 JomForm 注册链接",
			Body: "无需密码登录 JomForm：\n\n" + "{link}\n\n" +
				"此链接有效期为 15 分钟，只能使用一次。点击它将创建您的账户并登录。\n\n" +
				"如果您没有请求此链接，可以忽略此邮件。\n"},
		Ta: {Subject: "உங்கள் JomForm பதிவு இணைப்பு",
			Body: "கடவுச்சொல் இல்லாமல் JomForm இல் உள்நுழைய:\n\n" + "{link}\n\n" +
				"இந்த இணைப்பு 15 நிமிடங்களுக்கு செல்லுபடியாகும், ஒருமுறை மட்டுமே பயன்படுத்தலாம். கிளிக் செய்தால் உங்கள் கணக்கு உருவாக்கப்பட்டு உள்நுழையும்.\n\n" +
				"நீங்கள் இதைக் கோரவில்லை என்றால், இந்த மின்னஞ்சலைப் புறக்கணிக்கலாம்.\n"},
		Ar: {Subject: "رابط تسجيل JomForm الخاص بك",
			Body: "تسجيل الدخول إلى JomForm بدون كلمة مرور:\n\n" + "{link}\n\n" +
				"هذا الرابط صالح لمدة 15 دقيقة ويمكن استخدامه مرة واحدة فقط. سيؤدي النقر عليه إلى إنشاء حسابك وتسجيل الدخول.\n\n" +
				"إذا لم تطلب هذا، يمكنك تجاهل هذا البريد.\n"},
	},

	Email2FACode: {
		En: {Subject: "Your JomForm 2FA backup code",
			Body: "Your one-time 2FA sign-in backup code for JomForm:\n\n" +
				"\t{code}\n\n" +
				"This code is valid for 10 minutes and can be used once. It replaces the code from your authenticator app.\n\n" +
				"If you were not trying to sign in, you can ignore this email.\n"},
		Ms: {Subject: "Kod sandaran 2FA JomForm anda",
			Body: "Kod sandaran sekali-pakai untuk log masuk 2FA JomForm anda:\n\n" +
				"\t{code}\n\n" +
				"Kod ini sah selama 10 minit dan hanya boleh digunakan sekali. Ia menggantikan kod dari aplikasi pengesah anda.\n\n" +
				"Jika anda tidak cuba log masuk, anda boleh abaikan emel ini.\n"},
		Zh: {Subject: "您的 JomForm 2FA 备份验证码",
			Body: "您的一次性 JomForm 2FA 登录备份验证码：\n\n" +
				"\t{code}\n\n" +
				"此验证码有效期为 10 分钟，只能使用一次。它可替代验证器应用中的验证码。\n\n" +
				"如果您没有尝试登录，可以忽略此邮件。\n"},
		Ta: {Subject: "உங்கள் JomForm 2FA காப்புக் குறியீடு",
			Body: "உங்கள் ஒரு முறை பயன்பாட்டு JomForm 2FA உள்நுழைவுக் காப்புக் குறியீடு:\n\n" +
				"\t{code}\n\n" +
				"இந்தக் குறியீடு 10 நிமிடங்களுக்குச் செல்லுபடியாகும், ஒருமுறை மட்டுமே பயன்படுத்தலாம். இது உங்கள் அங்கீகாரப் பயன்பாட்டின் குறியீட்டை மாற்றுகிறது.\n\n" +
				"நீங்கள் உள்நுழைய முயற்சிக்கவில்லை என்றால், இந்த மின்னஞ்சலைப் புறக்கணிக்கலாம்.\n"},
		Ar: {Subject: "رمز النسخ الاحتياطي لـ 2FA في JomForm",
			Body: "رمز تسجيل الدخول الاحتياطي لمرة واحدة لـ 2FA في JomForm:\n\n" +
				"\t{code}\n\n" +
				"هذا الرمز صالح لمدة 10 دقائق ويمكن استخدامه مرة واحدة. وهو يحل محل الرمز من تطبيق المصادقة.\n\n" +
				"إذا لم تكن تحاول تسجيل الدخول، يمكنك تجاهل هذا البريد.\n"},
	},

	TwoFAActivated: {
		En: {Subject: "2FA is now active on your JomForm account",
			Body: "Two-factor authentication (2FA) is now ACTIVE on your JomForm account. ✓\n\n" +
				"On your next sign-in you will need to confirm a 6-digit code from your authenticator app.\n\n" +
				"If you lose access to your authenticator app, you can request a one-time backup code by email\n" +
				"using the \"Send a code via email\" link on the 2FA step. Note: an emailed code is an easier but\n" +
				"slightly weaker recovery path than TOTP alone — keep your mailbox secure.\n"},
		Ms: {Subject: "2FA kini aktif pada akaun JomForm anda",
			Body: "Pengesahan dua faktor (2FA) kini AKTIF pada akaun JomForm anda. ✓\n\n" +
				"Pada log masuk seterusnya, anda perlu mengesahkan kod 6-angka daripada aplikasi pengesah anda.\n\n" +
				"Jika anda kehilangan akses kepada aplikasi pengesah, anda boleh meminta kod sandaran sekali-pakai\n" +
				"dihantar ke emel ini melalui pautan \"Hantar kod melalui emel\" pada langkah 2FA. Nota: kod emel\n" +
				"adalah laluan pemulihan yang lebih mudah tetapi lebih lemah sedikit berbanding TOTP sahaja — layan\n" +
				"peti masuk emel anda dengan selamat.\n"},
		Zh: {Subject: "2FA 已在您的 JomForm 账户启用",
			Body: "双重验证（2FA）现已在您的 JomForm 账户上启用。✓\n\n" +
				"下次登录时，您需要确认验证器应用中的 6 位验证码。\n\n" +
				"如果您无法访问验证器应用，可以通过 2FA 步骤中的“通过电子邮件发送验证码”链接\n" +
				"请求一次性备份验证码。注意：邮件验证码是更便捷但安全性略低的恢复方式，\n" +
				"请妥善保管您的邮箱。\n"},
		Ta: {Subject: "உங்கள் JomForm கணக்கில் 2FA இப்போது செயலில் உள்ளது",
			Body: "இரு-காரணி அங்கீகாரம் (2FA) இப்போது உங்கள் JomForm கணக்கில் செயலில் உள்ளது. ✓\n\n" +
				"அடுத்த உள்நுழைவில், உங்கள் அங்கீகாரப் பயன்பாட்டின் 6 இலக்கக் குறியீட்டை உறுதிப்படுத்த வேண்டும்.\n\n" +
				"உங்கள் அங்கீகாரப் பயன்பாட்டை அணுக முடியாவிட்டால், 2FA படியில் உள்ள \"மின்னஞ்சல் வழியாகக்\n" +
				"குறியீடு அனுப்பு\" இணைப்பைப் பயன்படுத்தி ஒரு முறை பயன்பாட்டுக் காப்புக் குறியீட்டைக் கோரலாம்.\n" +
				"குறிப்பு: மின்னஞ்சல் குறியீடு எளிதானது ஆனால் TOTP-ஐ விட சற்று பலவீனமானது — உங்கள்\n" +
				"மின்னஞ்சல் பெட்டியைப் பாதுகாப்பாக வைத்திருங்கள்.\n"},
		Ar: {Subject: "تم تفعيل 2FA على حساب JomForm الخاص بك",
			Body: "المصادقة الثنائية (2FA) نشطة الآن على حساب JomForm الخاص بك. ✓\n\n" +
				"عند تسجيل الدخول التالي، ستحتاج إلى تأكيد رمز مكوّن من 6 أرقام من تطبيق المصادقة.\n\n" +
				"إذا فقدت الوصول إلى تطبيق المصادقة، يمكنك طلب رمز نسخ احتياطي لمرة واحدة عبر البريد\n" +
				"عبر رابط \"إرسال رمز عبر البريد الإلكتروني\" في خطوة 2FA. ملاحظة: الرمز المُرسل بالبريد\n" +
				"طريق استرداد أسهل لكنه أضعف قليلاً من TOTP وحده — حافظ على أمان بريدك.\n"},
	},

	SocialConnect: {
		En: {Subject: "Your JomForm account connection link",
			Body: "Please verify your email address to link your social sign-in to your JomForm account.\n\n" +
				"Tap the link below (valid for 24 hours):\n\n" +
				"{link}\n\n" +
				"If you didn't request this, you can ignore this email.\n"},
		Ms: {Subject: "Pautan penghubung akaun JomForm anda",
			Body: "Sila sahkan alamat emel anda untuk menghubungkan akaun log masuk sosial anda ke akaun JomForm anda.\n\n" +
				"Tap pautan berikut (sah selama 24 jam):\n\n" +
				"{link}\n\n" +
				"Jika anda tidak meminta ini, anda boleh abaikan emel ini.\n"},
		Zh: {Subject: "您的 JomForm 账户关联链接",
			Body: "请验证您的邮箱地址，以将您的社交登录关联到 JomForm 账户。\n\n" +
				"点击以下链接（有效期 24 小时）：\n\n" +
				"{link}\n\n" +
				"如果您没有请求此项，可以忽略此邮件。\n"},
		Ta: {Subject: "உங்கள் JomForm கணக்கு இணைப்பு இணைப்பு",
			Body: "உங்கள் சமூக உள்நுழைவை JomForm கணக்குடன் இணைக்க உங்கள் மின்னஞ்சல் முகவரியைச் சரிபார்க்கவும்.\n\n" +
				"கீழே உள்ள இணைப்பைத் தட்டவும் (24 மணி நேரம் செல்லுபடியாகும்):\n\n" +
				"{link}\n\n" +
				"நீங்கள் இதைக் கோரவில்லை என்றால், இந்த மின்னஞ்சலைப் புறக்கணிக்கலாம்.\n"},
		Ar: {Subject: "رابط ربط حساب JomForm الخاص بك",
			Body: "يرجى التحقق من عنوان بريدك الإلكتروني لربط تسجيل الدخول الاجتماعي بحسابك في JomForm.\n\n" +
				"انقر على الرابط التالي (صالح لمدة 24 ساعة):\n\n" +
				"{link}\n\n" +
				"إذا لم تطلب ذلك، يمكنك تجاهل هذا البريد.\n"},
	},

	// RFC 8628 device authorisation (card t_9a6cc85f). This email IS the consent
	// screen for a headless MCP onboarding: the reader has to be able to tell
	// what is asking, what it will be allowed to do, where the request came
	// from, and what to do if it was not them. Hence the client / access / from
	// / time block, and hence the explicit "do not share this code" line — the
	// code is a bearer secret that a token is minted from.
	//
	// Card t_4f3ab16e — the SECOND half of the same report ("dalam email tak
	// dinyatakan code pun"): the code was one line inside the paragraph block, so
	// a reader scanning for a big code did not find one. It now sits in a block
	// of its own and the link is a button, via the two line markers the branded
	// renderer understands (internal/email/branded.go):
	//
	//	[[code:VALUE]]          → a large, monospaced, centred code block
	//	[[button:LABEL|URL]]    → a button (with the URL printed under it)
	//
	// The markers are LINE-level and are rendered back to plain text by
	// PlainTextBody for the multipart fallback, so the text part still reads
	// "KX9M-X5BS" on its own line and "Approve or deny: https://…" — see
	// email/device_code_mail_t_4f3ab16e_test.go, which asserts the rendered HTML
	// carries the ACTUAL code for a fixture, in all five locales.
	DeviceAuthorization: {
		En: {Subject: "Approve an AI agent connection to JomForm",
			Body: "A device is asking to connect to your JomForm account as an AI agent.\n\n" +
				"Client : {client}\n" +
				"Access : {scope}\n" +
				"From   : {where} ({ip})\n" +
				"Time   : {time}\n\n" +
				"[[code:{code}]]\n" +
				"Enter it on jomform.com, or just open the button below.\n\n" +
				"[[button:Approve or deny|{link}]]\n\n" +
				"The code expires in 10 minutes. If this was not you, do not share this code — deny the request instead.\n"},
		Ms: {Subject: "Luluskan sambungan ejen AI ke JomForm",
			Body: "Sesebuah peranti meminta untuk menyambung ke akaun JomForm anda sebagai ejen AI.\n\n" +
				"Klien  : {client}\n" +
				"Akses  : {scope}\n" +
				"Dari   : {where} ({ip})\n" +
				"Masa   : {time}\n\n" +
				"[[code:{code}]]\n" +
				"Masukkannya di jomform.com, atau buka butang di bawah.\n\n" +
				"[[button:Luluskan atau tolak|{link}]]\n\n" +
				"Kod ini tamat tempoh dalam 10 minit. Jika ini bukan anda, jangan kongsi kod ini — tolak permintaan itu.\n"},
		Zh: {Subject: "批准 AI 代理连接到 JomForm",
			Body: "有设备请求以 AI 代理身份连接到您的 JomForm 账户。\n\n" +
				"客户端：{client}\n" +
				"权限：{scope}\n" +
				"来源：{where}（{ip}）\n" +
				"时间：{time}\n\n" +
				"[[code:{code}]]\n" +
				"在 jomform.com 输入此代码，或直接打开下面的按钮。\n\n" +
				"[[button:批准或拒绝|{link}]]\n\n" +
				"代码将在 10 分钟后失效。如果这不是您本人操作，请勿分享此代码 — 请拒绝该请求。\n"},
		Ta: {Subject: "JomForm க்கு AI முகவர் இணைப்பை அனுமதிக்கவும்",
			Body: "ஒரு சாதனம் AI முகவராக உங்கள் JomForm கணக்குடன் இணைக்கக் கோருகிறது.\n\n" +
				"கிளையன்ட்: {client}\n" +
				"அணுகல்: {scope}\n" +
				"இருப்பிடம்: {where} ({ip})\n" +
				"நேரம்: {time}\n\n" +
				"[[code:{code}]]\n" +
				"jomform.com இல் இதை உள்ளிடவும், அல்லது கீழே உள்ள பொத்தானைத் திறக்கவும்.\n\n" +
				"[[button:அனுமதிக்கவும் அல்லது நிராகரிக்கவும்|{link}]]\n\n" +
				"குறியீடு 10 நிமிடங்களில் காலாவதியாகும். இது நீங்கள் இல்லை என்றால், இந்தக் குறியீட்டைப் பகிர வேண்டாம் — கோரிக்கையை நிராகரிக்கவும்.\n"},
		Ar: {Subject: "الموافقة على اتصال وكيل ذكاء اصطناعي بـ JomForm",
			Body: "جهاز يطلب الاتصال بحساب JomForm الخاص بك كوكيل ذكاء اصطناعي.\n\n" +
				"العميل: {client}\n" +
				"الوصول: {scope}\n" +
				"من: {where} ({ip})\n" +
				"الوقت: {time}\n\n" +
				"[[code:{code}]]\n" +
				"أدخله في jomform.com، أو افتح الزر أدناه.\n\n" +
				"[[button:الموافقة أو الرفض|{link}]]\n\n" +
				"ينتهي الرمز خلال 10 دقائق. إذا لم تكن أنت، فلا تشارك هذا الرمز — ارفض الطلب بدلاً من ذلك.\n"},
	},

	WorkspaceInvite: {
		En: {Subject: "Invitation to join a JomForm workspace",
			Body: "You've been invited to join a JomForm workspace.\n\n" +
				"Click the link below to create an account and join the workspace:\n\n" +
				"{link}\n\n" +
				"If you weren't expecting this invitation, you can ignore this email.\n"},
		Ms: {Subject: "Jemputan menyertai workspace JomForm",
			Body: "Anda telah dijemput untuk menyertai workspace JomForm.\n\n" +
				"Klik pautan berikut untuk mendaftar akaun dan menyertai workspace:\n\n" +
				"{link}\n\n" +
				"Jika anda tidak mengharapkan jemputan ini, anda boleh abaikan emel ini.\n"},
		Zh: {Subject: "加入 JomForm 空间的邀请",
			Body: "您已被邀请加入 JomForm 空间。\n\n" +
				"点击以下链接注册账户并加入空间：\n\n" +
				"{link}\n\n" +
				"如果您没有预期收到此邀请，可以忽略此邮件。\n"},
		Ta: {Subject: "JomForm இடத்தில் சேர அழைப்பு",
			Body: "JomForm இடத்தில் சேர உங்களை அழைத்துள்ளோம்.\n\n" +
				"கணக்கைப் பதிவுசெய்து இடத்தில் சேர பின்வரும் இணைப்பைக் கிளிக் செய்யவும்:\n\n" +
				"{link}\n\n" +
				"இந்த அழைப்பை நீங்கள் எதிர்பார்க்கவில்லை என்றால், இந்த மின்னஞ்சலைப் புறக்கணிக்கலாம்.\n"},
		Ar: {Subject: "دعوة للانضمام إلى مساحة JomForm",
			Body: "تمت دعوتك للانضمام إلى مساحة JomForm.\n\n" +
				"انقر على الرابط التالي لإنشاء حساب والانضمام إلى المساحة:\n\n" +
				"{link}\n\n" +
				"إذا لم تكن تتوقع هذه الدعوة، يمكنك تجاهل هذا البريد.\n"},
	},

	FeedbackNew: {
		En: {Subject: "New feedback received",
			Body: "New feedback received.\n\n" +
				"From    : {from}\n" +
				"Status  : {status}\n" +
				"Subject : {subject}\n\n" +
				"{excerpt}\n\n" +
				"Reply at {link}\n"},
		Ms: {Subject: "Maklum balas baharu diterima",
			Body: "Maklum balas baharu diterima.\n\n" +
				"Dari    : {from}\n" +
				"Status  : {status}\n" +
				"Tajuk   : {subject}\n\n" +
				"{excerpt}\n\n" +
				"Balas di {link}\n"},
		Zh: {Subject: "收到新的反馈",
			Body: "收到新的反馈。\n\n" +
				"来自   : {from}\n" +
				"状态   : {status}\n" +
				"主题   : {subject}\n\n" +
				"{excerpt}\n\n" +
				"在此回复 {link}\n"},
		Ta: {Subject: "புதிய கருத்து பெறப்பட்டது",
			Body: "புதிய கருத்து பெறப்பட்டது.\n\n" +
				"அனுப்பியவர் : {from}\n" +
				"நிலை       : {status}\n" +
				"தலைப்பு    : {subject}\n\n" +
				"{excerpt}\n\n" +
				"{link} இல் பதிலளிக்கவும்\n"},
		Ar: {Subject: "تم استلام ملاحظات جديدة",
			Body: "تم استلام ملاحظات جديدة.\n\n" +
				"من      : {from}\n" +
				"الحالة  : {status}\n" +
				"الموضوع : {subject}\n\n" +
				"{excerpt}\n\n" +
				"الرد على {link}\n"},
	},

	FeedbackReply: {
		En: {Subject: "New reply on your feedback",
			Body: "We have replied to your feedback.\n\n" +
				"Subject : {subject}\n\n" +
				"{excerpt}\n\n" +
				"View the thread at {link}\n"},
		Ms: {Subject: "Balasan baharu pada maklum balas anda",
			Body: "Kami telah membalas maklum balas anda.\n\n" +
				"Tajuk   : {subject}\n\n" +
				"{excerpt}\n\n" +
				"Lihat thread di {link}\n"},
		Zh: {Subject: "您收到的反馈有新回复",
			Body: "我们已回复您的反馈。\n\n" +
				"主题   : {subject}\n\n" +
				"{excerpt}\n\n" +
				"在此查看讨论 {link}\n"},
		Ta: {Subject: "உங்கள் கருத்துக்கு புதிய பதில்",
			Body: "உங்கள் கருத்துக்கு நாங்கள் பதிலளித்துள்ளோம்.\n\n" +
				"தலைப்பு : {subject}\n\n" +
				"{excerpt}\n\n" +
				"உரையாடலை {link} இல் பார்க்கவும்\n"},
		Ar: {Subject: "رد جديد على ملاحظاتك",
			Body: "لقد قمنا بالرد على ملاحظاتك.\n\n" +
				"الموضوع : {subject}\n\n" +
				"{excerpt}\n\n" +
				"عرض المحادثة على {link}\n"},
	},

	// Card t_94e5cca3: the phase-1 dormancy nudge. It is platform-user mail
	// (resolved from the recipient's users.lang, like every other message to a
	// merchant) and it promises ONE thing only: the forms are still online. It
	// deliberately makes no claim about deletion, because phases 2/3 of the
	// policy are separate decisions and an email must not promise what the
	// product has not decided.
	DormancyNudge: {
		En: {Subject: "Your JomForm form is still online",
			Body: "Hi,\n\n" +
				"Just a check-in: the forms in your JomForm workspace \"{workspace}\" are still online and still accepting responses. Nothing has changed there in {days} days.\n\n" +
				"You do not need to do anything. If you have finished with them, you can unpublish or delete a form yourself from your dashboard.\n\n" +
				"This is the only reminder about activity we will send you in the next 90 days.\n"},
		Ms: {Subject: "Borang JomForm anda masih dalam talian",
			Body: "Hai,\n\n" +
				"Sekadar bertanya khabar: borang dalam ruang kerja JomForm anda \"{workspace}\" masih dalam talian dan masih menerima respons. Tiada perubahan di situ sejak {days} hari.\n\n" +
				"Anda tidak perlu buat apa-apa. Jika sudah selesai, anda boleh nyahterbit atau padam borang itu sendiri dari papan pemuka anda.\n\n" +
				"Ini satu-satunya peringatan tentang aktiviti yang akan kami hantar dalam 90 hari akan datang.\n"},
		Zh: {Subject: "您的 JomForm 表单仍然在线",
			Body: "您好，\n\n" +
				"只是问候一下：您的 JomForm 工作区“{workspace}”中的表单仍然在线，并且仍在接收回复。那里已经 {days} 天没有任何变更。\n\n" +
				"您不需要做任何事。如果已经用完，您可以在控制台中自行取消发布或删除表单。\n\n" +
				"这是未来 90 天内我们唯一一次关于活跃度的提醒。\n"},
		Ta: {Subject: "உங்கள் JomForm படிவம் இன்னும் இணையத்தில் உள்ளது",
			Body: "வணக்கம்,\n\n" +
				"ஒரு நினைவூட்டல்: உங்கள் JomForm பணியிடமான \"{workspace}\" இல் உள்ள படிவங்கள் இன்னும் இணையத்தில் உள்ளன, பதில்களையும் பெற்றுக்கொண்டிருக்கின்றன. {days} நாட்களாக அங்கு எந்த மாற்றமும் இல்லை.\n\n" +
				"நீங்கள் எதுவும் செய்யத் தேவையில்லை. வேலை முடிந்திருந்தால், உங்கள் டாஷ்போர்டில் இருந்து படிவத்தை வெளியிடாமல் செய்யலாம் அல்லது நீக்கலாம்.\n\n" +
				"அடுத்த 90 நாட்களில் செயல்பாடு குறித்து நாங்கள் அனுப்பும் ஒரே நினைவூட்டல் இதுவே.\n"},
		Ar: {Subject: "نموذج JomForm الخاص بك لا يزال منشوراً",
			Body: "مرحباً،\n\n" +
				"مجرد تذكير: النماذج في مساحة عملك \"{workspace}\" على JomForm لا تزال منشورة ولا تزال تستقبل الردود. لم يتغير شيء هناك منذ {days} يوماً.\n\n" +
				"لا حاجة لفعل أي شيء. إذا انتهيت منها، يمكنك إلغاء نشر النموذج أو حذفه بنفسك من لوحة التحكم.\n\n" +
				"هذا هو التذكير الوحيد الذي سنرسله عن النشاط خلال الـ 90 يوماً القادمة.\n"},
	},

	// ── buyer-facing mail (resolved from the form/workspace axis) ────────────

	Receipt: {
		En: {Subject: "Payment receipt — {{ref_code}}",
			Body: "Thank you for your payment!\n\n" +
				"JomForm payment receipt\n" +
				"Reference : {{ref_code}}\n" +
				"Amount    : RM {{amount}}\n" +
				"Item      : {{items}}\n" +
				"Status    : Payment received\n\n" +
				"Keep this email as your official receipt.\n" +
				"Form purchased: {{form_url}}"},
		Ms: {Subject: "Resit pembayaran — {{ref_code}}",
			Body: "Terima kasih atas pembayaran anda!\n\n" +
				"Resit pembayaran JomForm\n" +
				"Rujukan : {{ref_code}}\n" +
				"Jumlah  : RM {{amount}}\n" +
				"Item    : {{items}}\n" +
				"Status  : Pembayaran diterima\n\n" +
				"Simpan emel ini sebagai resit rasmi anda.\n" +
				"Borang dibeli: {{form_url}}"},
		Zh: {Subject: "付款收据 — {{ref_code}}",
			Body: "感谢您的付款！\n\n" +
				"JomForm 付款收据\n" +
				"参考编号 : {{ref_code}}\n" +
				"金额     : RM {{amount}}\n" +
				"项目     : {{items}}\n" +
				"状态     : 已收到付款\n\n" +
				"请保存此邮件作为您的正式收据。\n" +
				"购买的表格：{{form_url}}"},
		Ta: {Subject: "கட்டண ரசீது — {{ref_code}}",
			Body: "உங்கள் கட்டணத்திற்கு நன்றி!\n\n" +
				"JomForm கட்டண ரசீது\n" +
				"குறிப்பு : {{ref_code}}\n" +
				"தொகை    : RM {{amount}}\n" +
				"பொருள்  : {{items}}\n" +
				"நிலை    : கட்டணம் பெறப்பட்டது\n\n" +
				"இந்த மின்னஞ்சலை உங்கள் அதிகாரப்பூர்வ ரசீதாக வைத்திருங்கள்.\n" +
				"வாங்கிய படிவம்: {{form_url}}"},
		Ar: {Subject: "إيصال الدفع — {{ref_code}}",
			Body: "شكراً لك على دفعتك!\n\n" +
				"إيصال دفع JomForm\n" +
				"المرجع : {{ref_code}}\n" +
				"المبلغ : RM {{amount}}\n" +
				"العنصر : {{items}}\n" +
				"الحالة : تم استلام الدفع\n\n" +
				"احتفظ بهذا البريد كإيصالك الرسمي.\n" +
				"النموذج المشترى: {{form_url}}"},
	},

	NotificationSale: {
		En: {Subject: "New sale — {{ref_code}}",
			Body: "New sale received!\n\n" +
				"Reference : {{ref_code}}\n" +
				"Amount    : RM {{amount}}\n" +
				"Item      : {{items}}\n" +
				"Buyer     : {{buyer_name}}\n" +
				"Form      : {{form_url}}"},
		Ms: {Subject: "Jualan baharu — {{ref_code}}",
			Body: "Jualan baharu diterima!\n\n" +
				"Rujukan : {{ref_code}}\n" +
				"Jumlah  : RM {{amount}}\n" +
				"Item    : {{items}}\n" +
				"Pembeli : {{buyer_name}}\n" +
				"Borang  : {{form_url}}"},
		Zh: {Subject: "新销售 — {{ref_code}}",
			Body: "收到新销售！\n\n" +
				"参考编号 : {{ref_code}}\n" +
				"金额     : RM {{amount}}\n" +
				"项目     : {{items}}\n" +
				"买家     : {{buyer_name}}\n" +
				"表单     : {{form_url}}"},
		Ta: {Subject: "புதிய விற்பனை — {{ref_code}}",
			Body: "புதிய விற்பனை பெறப்பட்டது!\n\n" +
				"குறிப்பு  : {{ref_code}}\n" +
				"தொகை     : RM {{amount}}\n" +
				"பொருள்   : {{items}}\n" +
				"வாங்கியவர் : {{buyer_name}}\n" +
				"படிவம்   : {{form_url}}"},
		Ar: {Subject: "بيع جديد — {{ref_code}}",
			Body: "تم استلام بيع جديد!\n\n" +
				"المرجع : {{ref_code}}\n" +
				"المبلغ : RM {{amount}}\n" +
				"العنصر : {{items}}\n" +
				"المشتري : {{buyer_name}}\n" +
				"النموذج : {{form_url}}"},
	},

	NotificationResponse: {
		En: {Subject: "New submission received",
			Body: "New submission received!\n\n" +
				"Name  : {{full_name}}\n" +
				"Email : {{email}}\n" +
				"Form  : {{form_url}}\n" +
				"Date  : {{date}}"},
		Ms: {Subject: "Pendaftaran baharu diterima",
			Body: "Pendaftaran baharu diterima!\n\n" +
				"Nama   : {{full_name}}\n" +
				"Emel   : {{email}}\n" +
				"Borang : {{form_url}}\n" +
				"Tarikh : {{date}}"},
		Zh: {Subject: "收到新的提交",
			Body: "收到新的提交！\n\n" +
				"姓名   : {{full_name}}\n" +
				"邮箱   : {{email}}\n" +
				"表单   : {{form_url}}\n" +
				"日期   : {{date}}"},
		Ta: {Subject: "புதிய சமர்ப்பிப்பு பெறப்பட்டது",
			Body: "புதிய சமர்ப்பிப்பு பெறப்பட்டது!\n\n" +
				"பெயர்  : {{full_name}}\n" +
				"மின்னஞ்சல் : {{email}}\n" +
				"படிவம் : {{form_url}}\n" +
				"தேதி   : {{date}}"},
		Ar: {Subject: "تم استلام إرسال جديد",
			Body: "تم استلام إرسال جديد!\n\n" +
				"الاسم  : {{full_name}}\n" +
				"البريد : {{email}}\n" +
				"النموذج : {{form_url}}\n" +
				"التاريخ : {{date}}"},
	},
}

// phrases holds the standalone strings the shared branded wrapper needs, plus
// the pieces a sender composes around merchant-authored content.
var phrases = map[PhraseKey]map[string]string{
	// The merchant copy of a receipt prefixes the buyer's own subject; the
	// merchant is addressed in the BUYER-facing language (the form/workspace
	// axis), never in the merchant's personal users.lang.
	PhraseMerchantPrefix: {
		En: "New sale — ",
		Ms: "Jualan baharu — ",
		Zh: "新销售 — ",
		Ta: "புதிய விற்பனை — ",
		Ar: "بيع جديد — ",
	},
	// Shown in a feedback notification when the filer left the subject empty.
	PhraseNoSubject: {
		En: "(no subject)",
		Ms: "(tanpa tajuk)",
		Zh: "（无主题）",
		Ta: "(தலைப்பு இல்லை)",
		Ar: "(بدون موضوع)",
	},
	// Branded wrapper footer: an unrequested email can be ignored.
	PhraseDisclaimer: {
		En: "If you did not request this email, you can safely ignore it.",
		Ms: "Jika anda tidak meminta emel ini, anda boleh abaikan sahaja.",
		Zh: "如果您没有请求此邮件，可以安全地忽略它。",
		Ta: "இந்த மின்னஞ்சலை நீங்கள் கோரவில்லை என்றால், அதைப் புறக்கணிக்கலாம்.",
		Ar: "إذا لم تطلب هذا البريد، يمكنك تجاهله بأمان.",
	},
	// Branded wrapper footer: the origin line. The shell declares the reader's
	// language, so every sentence inside it follows that declaration — the
	// dashboard's own landing footer carries the same five
	// (web/src/components/i18nDict.ts "land.footer").
	PhraseMadeIn: {
		En: "JomForm — Made in Malaysia.",
		Ms: "JomForm — Dibuat di Malaysia.",
		Zh: "JomForm — 马来西亚制造。",
		Ta: "JomForm — மலேசியாவில் உருவாக்கப்பட்டது.",
		Ar: "JomForm — صُنع في ماليزيا.",
	},
	// An OAuth client registered through Dynamic Client Registration without a
	// client_name. The consent email must still say who is asking.
	PhraseUnnamedClient: {
		En: "an unregistered client (auto-registered without a name)",
		Ms: "klien tidak berdaftar (didaftar automatik tanpa nama)",
		Zh: "未注册的客户端（自动注册，无名称）",
		Ta: "பதிவு செய்யப்படாத கிளையன்ட் (பெயர் இல்லாமல் தானாகப் பதிவு)",
		Ar: "عميل غير مسجل (سُجّل تلقائياً بدون اسم)",
	},
	// The two OAuth scopes in plain words, for the consent email. The reader is
	// being asked to hand an AI agent a credential; "readwrite" is not an answer
	// to "what will it be able to do".
	PhraseScopeRead: {
		En: "read-only — can view your forms, products and sales; cannot change anything",
		Ms: "baca sahaja — boleh melihat borang, produk dan jualan anda; tidak boleh mengubah apa-apa",
		Zh: "只读 — 可以查看您的表单、产品和销售；不能更改任何内容",
		Ta: "படிக்க மட்டும் — உங்கள் படிவங்கள், தயாரிப்புகள் மற்றும் விற்பனைகளைப் பார்க்கலாம்; எதையும் மாற்ற முடியாது",
		Ar: "للقراءة فقط — يمكنه عرض نماذجك ومنتجاتك ومبيعاتك؛ لا يمكنه تغيير أي شيء",
	},
	PhraseScopeReadWrite: {
		En: "read and write — can create, change and delete your forms, products and sales",
		Ms: "baca dan tulis — boleh mencipta, mengubah dan memadam borang, produk dan jualan anda",
		Zh: "读写 — 可以创建、更改和删除您的表单、产品和销售",
		Ta: "படிக்க மற்றும் எழுத — உங்கள் படிவங்கள், தயாரிப்புகள் மற்றும் விற்பனைகளை உருவாக்க, மாற்ற, நீக்க முடியும்",
		Ar: "قراءة وكتابة — يمكنه إنشاء نماذجك ومنتجاتك ومبيعاتك وتغييرها وحذفها",
	},
	// Where a device authorisation came from, when no edge reported a city.
	PhraseUnknownLocation: {
		En: "location not reported by the network",
		Ms: "lokasi tidak dilaporkan oleh rangkaian",
		Zh: "网络未报告位置",
		Ta: "பிணையம் இருப்பிடத்தைத் தெரிவிக்கவில்லை",
		Ar: "لم تُبلغ الشبكة عن الموقع",
	},
	// Branded wrapper footer: copyright. JomForm's code is MIT-licensed, so the
	// footer states the licence rather than a blanket reservation of rights.
	PhraseCopyright: {
		En: "© JomForm — open source under the MIT licence.",
		Ms: "© JomForm — sumber terbuka di bawah lesen MIT.",
		Zh: "© JomForm — 开源，MIT 许可。",
		Ta: "© JomForm — MIT உரிமத்தின் கீழ் திறந்த மூல.",
		Ar: "© JomForm — مفتوح المصدر بموجب رخصة MIT.",
	},
}
