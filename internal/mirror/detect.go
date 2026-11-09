package mirror

import (
	"context"
	"fmt"
	"time"
)

// Detect 探测"用哪个档位"，给 auto 用（方案 §6.3 第 4 条：普通命令只探 2 个候选）。
//
// 规则：先探候选 A（官方），再探候选 B（国内）；谁先成功用谁，A 成功就不再探 B ——
// 国外环境多花 0 次请求，国内环境多花一次 500ms 以内的 HEAD。
func Detect(ctx context.Context, prober *Prober, officialURL, cnURL string) (string, Result, error) {
	cache := prober.Load()
	remember := func(res Result) {
		cache.Results[res.URL] = res
		_ = prober.Save(cache)
	}
	if officialURL != "" {
		res := prober.ProbeOne(ctx, officialURL)
		remember(res)
		if res.OK {
			return "default", res, nil
		}
	}
	if cnURL != "" {
		res := prober.ProbeOne(ctx, cnURL)
		remember(res)
		if res.OK {
			return "cn", res, nil
		}
		return "", res, fmt.Errorf("国外与国内镜像都不可达（%s / %s）：用 PK_MIRROR 指向内网源，或检查代理", officialURL, cnURL)
	}
	return "", Result{}, fmt.Errorf("没有可探测的候选")
}

// ProfileOf 把档位名归一：auto 交给 Detect，其他原样返回。
func ProfileOf(profile string) string {
	if profile == "" || profile == AutoProfile {
		return ""
	}
	return profile
}

// CachedProfile 从探测缓存里挑一个已知可用的档位（不发网络请求）。
//
// 用于 `pk mirror list` 与 install 的前置判断：缓存里官方源 OK 就用 default，
// 否则回落到 cn。缓存 TTL 由调用方保证（Prober.Load 只读文件）。
func CachedProfile(prober *Prober, officialURL, cnURL string, now time.Time, ttl time.Duration) string {
	cache := prober.Load()
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	fresh := func(url string) bool {
		res, ok := cache.Results[url]
		return ok && res.OK && now.Sub(res.CheckedAt) < ttl
	}
	if officialURL != "" && fresh(officialURL) {
		return "default"
	}
	if cnURL != "" && fresh(cnURL) {
		return "cn"
	}
	return ""
}
