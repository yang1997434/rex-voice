package qwen

// CNY per 1M tokens, 北京地域（https://help.aliyun.com/zh/model-studio/model-pricing ，
// 2026-11-01 调研 docs/research/2026-11-01-realtime-providers.md），上线前对照官网核。
// 音频和文本输出分开计费；100 万 token 免费额度不在这里扣。
type price struct{ AudioIn, AudioOut, TextIn, TextOut float64 }

var prices = map[string]price{
	"qwen3.8-omni-flash-realtime": {AudioIn: 6, AudioOut: 12, TextIn: 1.5, TextOut: 4.5},
}

const cnyPerUSD = 7.2 // 元 → 美元，固定汇率，usage.log 统一记美元

// cost returns the estimated USD of one response, and false when the model has no price entry.
func cost(u usage) (float64, bool) {
	p, ok := prices[u.Model]
	if !ok {
		return 0, false
	}
	cny := float64(u.AudioIn)*p.AudioIn + float64(u.TextIn)*p.TextIn + float64(u.AudioOut)*p.AudioOut + float64(u.TextOut)*p.TextOut
	return cny / 1e6 / cnyPerUSD, true
}
