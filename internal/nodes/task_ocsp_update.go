// Copyright 2022 Liuxiangchao iwind.liu@gmail.com. All rights reserved.

package nodes

import (
	"github.com/TeaOSLab/EdgeCommon/pkg/rpc/pb"
	"github.com/TeaOSLab/EdgeNode/internal/events"
	"github.com/TeaOSLab/EdgeNode/internal/goman"
	"github.com/TeaOSLab/EdgeNode/internal/remotelogs"
	"github.com/TeaOSLab/EdgeNode/internal/rpc"
	"github.com/iwind/TeaGo/Tea"
	"time"
)

var sharedOCSPTask = NewOCSPUpdateTask()

func init() {
	events.On(events.EventLoaded, func() {
		sharedOCSPTask.version = sharedNodeConfig.OCSPVersion

		goman.New(func() {
			sharedOCSPTask.Start()
		})
	})
	events.On(events.EventQuit, func() {
		sharedOCSPTask.Stop()
	})

}

// OCSPUpdateTask 更新OCSP任务
type OCSPUpdateTask struct {
	version int64

	ticker *time.Ticker
}

func NewOCSPUpdateTask() *OCSPUpdateTask {
	var ticker = time.NewTicker(1 * time.Minute)
	if Tea.IsTesting() {
		ticker = time.NewTicker(10 * time.Second)
	}
	return &OCSPUpdateTask{
		ticker: ticker,
	}
}

func (this *OCSPUpdateTask) Start() {
	for range this.ticker.C {
		err := this.Loop()
		if err != nil {
			remotelogs.Warn("OCSPUpdateTask", "update ocsp failed: "+err.Error())
		}
	}
}

func (this *OCSPUpdateTask) Loop() error {
	rpcClient, err := rpc.SharedRPC()
	if err != nil {
		return err
	}

	resp, err := rpcClient.SSLCertService().ListUpdatedSSLCertOCSP(rpcClient.Context(), &pb.ListUpdatedSSLCertOCSPRequest{
		Version: this.version,
		Size:    100,
	})
	if err != nil {
		return err
	}

	for _, ocsp := range resp.SslCertOCSP {
		// 更新OCSP
		sharedNodeConfig.UpdateCertOCSP(ocsp.SslCertId, ocsp.Ocsp)

		// 修改版本
		this.version = ocsp.Version
	}

	return nil
}

func (this *OCSPUpdateTask) Stop() {
	this.ticker.Stop()
}

func (this *OCSPUpdateTask) updateOCSP(certId int64, ocsp []byte) {
	var config = sharedNodeConfig
	if config == nil {
		return
	}

}
