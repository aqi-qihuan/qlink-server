package enums

type AuthType string

const (
	AUTH_DEFAULT    AuthType = "DEFAULT"
	AUTH_REALNAME   AuthType = "REALNAME"
	AUTH_ENTERPRISE AuthType = "ENTERPRISE"
	AUTH_GOLD       AuthType = "GOLD"   // 黄金会员（SECOND 商品）
	AUTH_BLACK      AuthType = "BLACK"  // 黑金会员（THIRD 商品）
)
