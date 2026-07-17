package extensionupdater

const (
	ProtocolVersion = 1
	UpdaterVersion  = "0.1.1"

	ExtensionID     = "hcnomejlhhefpnhljgcclhggoljokimn"
	ExtensionOrigin = "chrome-extension://" + ExtensionID + "/"
	NativeHostName  = "com.octopus.extension_updater"

	GitHubOwner           = "deshuai719"
	GitHubRepo            = "octopus"
	ReleaseTagPrefix      = "extension-v"
	ReleaseManifestAsset  = "octopus-extension-update.json"
	ReleaseSignatureAsset = "octopus-extension-update.json.sig"

	ReleasePublicKeyBase64  = "7JPdwqCSsVXeFGiYT+aDlSEhdAYz08M21LmjDsdI2Sc="
	ManifestPublicKeyBase64 = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA2uL4tYSNPBVzj3XlRNU6OzUHZFwjL8ydkmAgOrR0bIoVDDZVqNYhftALM4Jej8atLn2+dQ27Ktn54aaEgnyz1u41LuLf4NX9NuNCOe6OcjFJLTgBS/328PB525eyawvXuieBN1dOpVB+e827ACEssj8lcRvBtLd8xsAoG6tPY0+1gshzookHfW2nDnuHrEcRVoQ0X1Z7zAjGaay8vsCgof5LRU7M+OXClmvTP2CKVkw760XCyZAVLuT6IoHAMyPqAP3/8SIxYbqHCfA+jMVU8UQ5/dN/7AyZ+hEL8cLjQPJbVmxYwTeNMb44RXurvXXielhPN0ENlS0hC3rA+XwGOwIDAQAB"
)
