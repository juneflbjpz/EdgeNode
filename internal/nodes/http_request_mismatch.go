// Copyright 2022 Liuxiangchao iwind.liu@gmail.com. All rights reserved. Official site: https://goedge.cn .

package nodes

import (
	"github.com/TeaOSLab/EdgeCommon/pkg/nodeutils"
	"github.com/TeaOSLab/EdgeCommon/pkg/serverconfigs"
	"github.com/TeaOSLab/EdgeCommon/pkg/serverconfigs/firewallconfigs"
	"github.com/TeaOSLab/EdgeNode/internal/ttlcache"
	"github.com/TeaOSLab/EdgeNode/internal/waf"
	"net/http"
	"time"
)

// 域名无匹配情况处理
func (this *HTTPRequest) doMismatch() {
	// 是否为健康检查
	var healthCheckKey = this.RawReq.Header.Get(serverconfigs.HealthCheckHeaderName)
	if len(healthCheckKey) > 0 {
		_, err := nodeutils.Base64DecodeMap(healthCheckKey)
		if err == nil {
			this.writer.WriteHeader(http.StatusOK)
			return
		}
	}

	// 是否已经在黑名单
	var remoteIP = this.RemoteAddr()
	if waf.SharedIPBlackList.Contains(waf.IPTypeAll, firewallconfigs.FirewallScopeGlobal, 0, remoteIP) {
		this.Close()
		return
	}

	// 根据配置进行相应的处理
	if sharedNodeConfig.GlobalServerConfig != nil && sharedNodeConfig.GlobalServerConfig.HTTPAll.MatchDomainStrictly {
		// 检查cc
		// TODO 可以在管理端配置是否开启以及最多尝试次数
		// 要考虑到服务在切换集群时，域名未生效状态时，用户访问的仍然是老集群中的节点，就会产生找不到域名的情况
		if len(remoteIP) > 0 {
			const maxAttempts = 100
			if ttlcache.SharedCache.IncreaseInt64("MISMATCH_DOMAIN:"+remoteIP, int64(1), time.Now().Unix()+60, false) > maxAttempts {
				// 在加入之前再次检查黑名单
				if !waf.SharedIPBlackList.Contains(waf.IPTypeAll, firewallconfigs.FirewallScopeGlobal, 0, remoteIP) {
					waf.SharedIPBlackList.Add(waf.IPTypeAll, firewallconfigs.FirewallScopeGlobal, 0, remoteIP, time.Now().Unix()+3600)
				}
			}
		}

		// 处理当前连接
		var httpAllConfig = sharedNodeConfig.GlobalServerConfig.HTTPAll
		var mismatchAction = httpAllConfig.DomainMismatchAction
		if mismatchAction != nil && mismatchAction.Code == "page" {
			if mismatchAction.Options != nil {
				this.writer.Header().Set("Content-Type", "text/html; charset=utf-8")
				this.writer.WriteHeader(mismatchAction.Options.GetInt("statusCode"))
				_, _ = this.writer.Write([]byte(mismatchAction.Options.GetString("contentHTML")))
			} else {
				http.Error(this.writer, "404 page not found: '"+this.URL()+"'", http.StatusNotFound)
			}
			return
		} else {
			http.Error(this.writer, "404 page not found: '"+this.URL()+"'", http.StatusNotFound)
			this.Close()
			return
		}
	}

	http.Error(this.writer, "404 page not found: '"+this.URL()+"'", http.StatusNotFound)
}
