package huangguoai

import "errors"

var (
	errBadPage = errors.New("作品页地址不被允许")
	errNoTitle = errors.New("作品页缺少标题")
)
