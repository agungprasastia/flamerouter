// Package qoder provides constants, cryptographic signing (COSY), and encoding routines for Qoder API integration.
package qoder

// Qoder API base URLs and endpoints ported from 9router/open-sse.
const (
	OpenAPIBase = "https://openapi.qoder.sh"
	CenterBase  = "https://center.qoder.sh"
	ChatBase    = "https://api3.qoder.sh"
	ChatBaseAlt = "https://api2.qoder.sh"
	LoginURL    = "https://qoder.com/device/selectAccounts"

	// Device flow endpoints.
	DeviceTokenURL  = OpenAPIBase + "/api/v1/deviceToken/poll"
	UserInfoURL     = OpenAPIBase + "/api/v1/userinfo"
	QuotaUsageURL   = OpenAPIBase + "/api/v2/quota/usage"
	RefreshTokenURL = CenterBase + "/algo/api/v3/user/refresh_token"

	// PAT token exchange endpoint.
	JobTokenExchangeURL = OpenAPIBase + "/api/v1/jobToken/exchange"

	// Inference endpoints (under /algo on api3.qoder.sh, all COSY-signed).
	ChatSigPath    = "/api/v2/service/pro/sse/agent_chat_generation"
	ChatURL        = ChatBase + "/algo" + ChatSigPath + "?FetchKeys=llm_model_result&AgentId=agent_common"
	ChatURLEncoded = ChatURL + "&Encode=1"
	ModelListURL   = ChatBase + "/algo/api/v2/model/list"

	// COSY header constants.
	IDEVersion   = "1.0.0"
	ClientType   = "5"
	DataPolicy   = "disagree"
	LoginVersion = "v2"
	MachineOS    = "x86_64_windows"
	MachineType  = "5"

	// RSAPublicKey for COSY encryption (extracted from Qoder IDE v0.9).
	RSAPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`
)

// ModelMap provides canonical model identifiers.
var ModelMap = map[string]string{
	// Tier models
	"auto":        "auto",
	"ultimate":    "ultimate",
	"performance": "performance",
	"efficient":   "efficient",
	"lite":        "lite",
	// Frontier models
	"qmodel":        "qmodel",
	"qmodel_latest": "qmodel_latest",
	"dmodel":        "dmodel",
	"dfmodel":       "dfmodel",
	"gm51model":     "gm51model",
	"kmodel":        "kmodel",
	"mmodel":        "mmodel",
}
