// Package i18n holds user-facing messages in English and Hindi.
//
// HINDI TEXT IS A DRAFT. The legal texts (notice, disclaimer, owner's declaration) must be
// reviewed together with the English originals in the lawyer review before public release.
// English is the reference version.
package i18n

import "fmt"

// Lang is "en" or "hi".
type Lang string

const (
	EN Lang = "en"
	HI Lang = "hi"
)

var msgs = map[string]map[Lang]string{
	"notice": {
		EN: `The Digital Personal Data Protection Act, 2023 applies to personal data in these books.
Data the AI reads is sent to your AI service provider for processing. You must have written
authorisation from the business before continuing.`,
		HI: `इन बहीखातों में मौजूद व्यक्तिगत डेटा पर डिजिटल व्यक्तिगत डेटा संरक्षण अधिनियम, 2023 लागू होता है।
AI जो डेटा पढ़ता है, वह प्रोसेसिंग के लिए आपके AI सेवा प्रदाता को भेजा जाता है। आगे बढ़ने से पहले
आपके पास व्यवसाय की लिखित अनुमति होनी चाहिए।`,
	},
	"disclaimer": {
		EN: `This tool cannot verify that an authorisation letter or email is genuine. It only stores
what you provide, encrypted on this PC. Legal responsibility for obtaining and holding valid
authorisation stays with you.`,
		HI: `यह टूल जाँच नहीं कर सकता कि अनुमति पत्र या ईमेल असली है या नहीं। यह केवल वही रखता है जो आप
देते हैं, इसी कंप्यूटर पर एन्क्रिप्ट करके। वैध अनुमति लेने और रखने की कानूनी ज़िम्मेदारी आपकी है।`,
	},
	"owner_declaration": {
		EN: `I declare that I am the owner of this business, or a partner/director authorised by it,
that these are the business's own books, and that I understand data the AI reads is sent to
my AI service provider for processing.`,
		HI: `मैं घोषणा करता/करती हूँ कि मैं इस व्यवसाय का/की मालिक हूँ, या इसके द्वारा अधिकृत साझेदार/निदेशक हूँ,
कि ये व्यवसाय के अपने बहीखाते हैं, और मैं समझता/समझती हूँ कि AI जो डेटा पढ़ता है वह प्रोसेसिंग के लिए
मेरे AI सेवा प्रदाता को भेजा जाता है।`,
	},
	"self_declaration": {
		EN: `I confirm that I hold written authorisation from this business to access and process its
accounting data with an AI assistant for the purpose agreed with them, and that I will produce
it if asked. I understand data the AI reads is sent to my AI service provider for processing.`,
		HI: `मैं पुष्टि करता/करती हूँ कि मेरे पास इस व्यवसाय की लिखित अनुमति है कि मैं उनके साथ तय उद्देश्य के लिए
AI सहायक से उनका लेखा डेटा देख और प्रोसेस कर सकूँ, और माँगे जाने पर मैं वह अनुमति दिखाऊँगा/दिखाऊँगी।
मैं समझता/समझती हूँ कि AI जो डेटा पढ़ता है वह प्रोसेसिंग के लिए मेरे AI सेवा प्रदाता को भेजा जाता है।`,
	},
	"self_declaration_all": {
		EN: `For this financial year, I confirm that I hold written authorisation from every business
whose books I connect with this tool, to access and process its accounting data with an AI
assistant for the purpose agreed with them, and that I will produce it if asked. I understand
data the AI reads is sent to my AI service provider for processing.`,
		HI: `इस वित्त वर्ष के लिए, मैं पुष्टि करता/करती हूँ कि जिस भी व्यवसाय के बहीखाते मैं इस टूल से जोड़ूँगा/जोड़ूँगी,
उसकी लिखित अनुमति मेरे पास है कि मैं उनके साथ तय उद्देश्य के लिए AI सहायक से उनका लेखा डेटा देख और प्रोसेस
कर सकूँ, और माँगे जाने पर मैं वह अनुमति दिखाऊँगा/दिखाऊँगी। मैं समझता/समझती हूँ कि AI जो डेटा पढ़ता है वह
प्रोसेसिंग के लिए मेरे AI सेवा प्रदाता को भेजा जाता है।`,
	},
	"owner_declaration_all": {
		EN: `For this financial year, I declare that every business whose books I connect with this tool
is my own business (I am its owner, or a partner/director authorised by it), and that I understand
data the AI reads is sent to my AI service provider for processing.`,
		HI: `इस वित्त वर्ष के लिए, मैं घोषणा करता/करती हूँ कि जिस भी व्यवसाय के बहीखाते मैं इस टूल से जोड़ूँगा/जोड़ूँगी,
वह मेरा अपना व्यवसाय है (मैं उसका/उसकी मालिक हूँ, या उसके द्वारा अधिकृत साझेदार/निदेशक हूँ), और मैं
समझता/समझती हूँ कि AI जो डेटा पढ़ता है वह प्रोसेसिंग के लिए मेरे AI सेवा प्रदाता को भेजा जाता है।`,
	},
	"self_heading":    {EN: "Your declaration:", HI: "आपकी घोषणा:"},
	"pick_company":    {EN: "Companies open in TallyPrime:", HI: "TallyPrime में खुली कंपनियाँ:"},
	"pick_number":     {EN: "Choose a company (number): ", HI: "कंपनी चुनें (संख्या): "},
	"owner_heading":   {EN: "Owner's declaration:", HI: "मालिक की घोषणा:"},
	"auth_file":       {EN: "Authorisation file: %s (%d bytes)", HI: "अनुमति फ़ाइल: %s (%d बाइट)"},
	"type_agree":      {EN: "Type %s to accept and record this authorisation: ", HI: "स्वीकार करने और अनुमति दर्ज करने के लिए %s टाइप करें: "},
	"not_accepted":    {EN: "not accepted; nothing recorded", HI: "स्वीकार नहीं किया गया; कुछ भी दर्ज नहीं हुआ"},
	"recorded":        {EN: "Recorded (encrypted) in %s. Valid for FY %s.", HI: "दर्ज किया गया (एन्क्रिप्टेड): %s। वित्त वर्ष %s के लिए मान्य।"},
	"company_line":    {EN: "Company: %s   GSTIN: %s   Source: %s   Financial year: %s", HI: "कंपनी: %s   GSTIN: %s   स्रोत: %s   वित्त वर्ष: %s"},
	"forget_header":   {EN: "This permanently deletes all local data for %q (%s):", HI: "यह %q (%s) का सारा स्थानीय डेटा हमेशा के लिए मिटा देगा:"},
	"forget_tally":    {EN: "Your data in TallyPrime is NOT touched. This cannot be undone.", HI: "TallyPrime में आपका डेटा नहीं छुआ जाएगा। यह वापस नहीं हो सकता।"},
	"forget_close":    {EN: "Close your AI app first if it is running (it may hold the database open).", HI: "पहले अपना AI ऐप बंद करें, अगर वह चल रहा है (वह डेटाबेस खुला रख सकता है)।"},
	"forget_confirm":  {EN: "Type the company name exactly to confirm: ", HI: "पुष्टि के लिए कंपनी का नाम हूबहू टाइप करें: "},
	"forget_mismatch": {EN: "name did not match; nothing deleted", HI: "नाम मेल नहीं खाया; कुछ भी नहीं मिटाया गया"},
	"forget_done":     {EN: "Deleted %s.", HI: "मिटा दिया गया: %s"},
	"tool_disclaimer": {EN: `Tally MCP by Guru is free software provided "as is" under the MIT licence, without any warranty.
It assists with accounting, reconciliation, audit checks and reports; it is not professional, tax or legal
advice. Back up your Tally data before any entry is posted. Check every output yourself. Audit findings need
the auditor's own judgment and sign-off. You are responsible for your clients' data and their authorisations.
Not affiliated with or endorsed by Tally Solutions Pvt. Ltd. Tally and TallyPrime are trademarks of Tally
Solutions Pvt. Ltd.`,
		HI: `Tally MCP by Guru एक मुफ़्त सॉफ़्टवेयर है, जो MIT लाइसेंस के तहत "जैसा है" उपलब्ध है, बिना किसी वारंटी के।
यह अकाउंटिंग, मिलान, ऑडिट जाँच और रिपोर्ट में सहायता करता है; यह पेशेवर, टैक्स या कानूनी सलाह नहीं है।
कोई भी एंट्री पोस्ट होने से पहले अपने Tally डेटा का बैकअप लें। हर आउटपुट स्वयं जाँचें। ऑडिट निष्कर्षों पर
ऑडिटर का अपना निर्णय और हस्ताक्षर आवश्यक है। अपने क्लाइंट के डेटा और उनकी अनुमति की ज़िम्मेदारी आपकी है।
Tally Solutions Pvt. Ltd. से संबद्ध या उनके द्वारा समर्थित नहीं। Tally और TallyPrime, Tally Solutions Pvt. Ltd. के ट्रेडमार्क हैं।`},
	"setup_title":    {EN: "Tally MCP by Guru — feature setup", HI: "Tally MCP by Guru — सुविधा सेटअप"},
	"setup_intro":    {EN: "Every feature is optional. Only what you switch on is loaded into your AI app.", HI: "हर सुविधा वैकल्पिक है। केवल वही AI ऐप में लोड होगा जो आप चालू करेंगे।"},
	"setup_profile":  {EN: "Choose a role profile (1-4)", HI: "भूमिका चुनें (1-4)"},
	"setup_modules":  {EN: "Modules (number to switch on/off, Enter when done):", HI: "मॉड्यूल (चालू/बंद करने के लिए संख्या, पूरा होने पर Enter):"},
	"setup_toggle":   {EN: "Toggle", HI: "बदलें"},
	"setup_later":    {EN: "  (available from phase %d)", HI: "  (चरण %d से उपलब्ध)"},
	"setup_privacy":  {EN: "Privacy mode: show party names to the AI as codes (P001...)? y/n", HI: "गोपनीयता मोड: AI को पार्टी के नाम कोड (P001...) में दिखाएँ? y/n"},
	"setup_readonly": {EN: "Read-only (hide all write tools)? y/n", HI: "केवल पढ़ने के लिए (सभी लिखने वाले टूल छिपाएँ)? y/n"},
	"setup_saved":    {EN: "Saved %s. Modules loading now: %s", HI: "सहेजा गया: %s। अभी लोड होने वाले मॉड्यूल: %s"},
	// Sent to the AI app at connection time when Hindi is chosen.
	"ai_instructions": {
		EN: "To connect or add a Tally company, call sync_now (with its name if given).",
		HI: "To connect or add a Tally company, call sync_now (with its name if given). The user prefers Hindi. Reply in simple Hindi (Devanagari). Keep amounts, ledger names and party codes exactly as the tools return them.",
	},
}

// ConfirmWords accepted when recording an authorisation.
var ConfirmWords = map[Lang][]string{EN: {"I AGREE"}, HI: {"मैं सहमत हूँ", "I AGREE"}}

// T returns the message for key in lang, falling back to English.
func T(lang Lang, key string, args ...any) string {
	m, ok := msgs[key]
	if !ok {
		return key
	}
	s := m[lang]
	if s == "" && key != "ai_instructions" {
		s = m[EN]
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// Of converts a config language string.
func Of(s string) Lang {
	if s == string(HI) {
		return HI
	}
	return EN
}
