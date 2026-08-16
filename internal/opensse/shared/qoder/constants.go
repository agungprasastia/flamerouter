package qoder

// Qoder API base URLs and endpoints ported from 9router/open-sse.
const (
	QODER_OPENAPI_BASE  = "https://openapi.qoder.sh"
	QODER_CENTER_BASE   = "https://center.qoder.sh"
	QODER_CHAT_BASE     = "https://api3.qoder.sh"
	QODER_CHAT_BASE_ALT = "https://api2.qoder.sh"
	QODER_LOGIN_URL     = "https://qoder.com/device/selectAccounts"

	// Device flow endpoints.
	QODER_DEVICE_TOKEN_URL  = QODER_OPENAPI_BASE + "/api/v1/deviceToken/poll"
	QODER_USERINFO_URL      = QODER_OPENAPI_BASE + "/api/v1/userinfo"
	QODER_QUOTA_USAGE_URL   = QODER_OPENAPI_BASE + "/api/v2/quota/usage"
	QODER_REFRESH_TOKEN_URL = QODER_CENTER_BASE + "/algo/api/v3/user/refresh_token"

	// PAT token exchange endpoint.
	QODER_JOB_TOKEN_EXCHANGE_URL = QODER_OPENAPI_BASE + "/api/v1/jobToken/exchange"

	// Inference endpoints (under /algo on api3.qoder.sh, all COSY-signed).
	QODER_CHAT_SIG_PATH    = "/api/v2/service/pro/sse/agent_chat_generation"
	QODER_CHAT_URL         = QODER_CHAT_BASE + "/algo" + QODER_CHAT_SIG_PATH + "?FetchKeys=llm_model_result&AgentId=agent_common"
	QODER_CHAT_URL_ENCODED = QODER_CHAT_URL + "&Encode=1"
	QODER_MODEL_LIST_URL   = QODER_CHAT_BASE + "/algo/api/v2/model/list"

	// COSY header constants.
	QODER_IDE_VERSION   = "1.0.0"
	QODER_CLIENT_TYPE   = "5"
	QODER_DATA_POLICY   = "disagree"
	QODER_LOGIN_VERSION = "v2"
	QODER_MACHINE_OS    = "x86_64_windows"
	QODER_MACHINE_TYPE  = "5"

	// RSA public key for COSY encryption (extracted from Qoder IDE v0.9).
	QODER_RSA_PUBLIC_KEY = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`
)

// QODER_MODEL_MAP provides canonical model identifiers.
var QODER_MODEL_MAP = map[string]string{
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
