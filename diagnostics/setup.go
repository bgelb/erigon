// Copyright 2024 The Erigon Authors
// This file is part of Erigon.
//
// Erigon is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Erigon is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with Erigon. If not, see <http://www.gnu.org/licenses/>.

package diagnostics

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/urfave/cli/v2"

	diaglib "github.com/ledgerwatch/erigon-lib/diagnostics"
	"github.com/ledgerwatch/erigon-lib/log/v3"
	"github.com/ledgerwatch/erigon/turbo/node"
)

var (
	diagnosticsDisabledFlag = "diagnostics.disabled"
	diagnosticsAddrFlag     = "diagnostics.endpoint.addr"
	diagnosticsPortFlag     = "diagnostics.endpoint.port"
	metricsHTTPFlag         = "metrics.addr"
	metricsPortFlag         = "metrics.port"
	pprofPortFlag           = "pprof.port"
	pprofAddrFlag           = "pprof.addr"
	diagnoticsSpeedTestFlag = "diagnostics.speedtest"
)

func Setup(ctx *cli.Context, node *node.ErigonNode, metricsMux *http.ServeMux, pprofMux *http.ServeMux) {
	if ctx.Bool(diagnosticsDisabledFlag) {
		return
	}

	var diagMux *http.ServeMux

	diagHost := ctx.String(diagnosticsAddrFlag)
	diagPort := ctx.Int(diagnosticsPortFlag)
	diagAddress := fmt.Sprintf("%s:%d", diagHost, diagPort)

	metricsHost := ctx.String(metricsHTTPFlag)
	metricsPort := ctx.Int(metricsPortFlag)
	metricsAddress := fmt.Sprintf("%s:%d", metricsHost, metricsPort)
	pprofHost := ctx.String(pprofAddrFlag)
	pprofPort := ctx.Int(pprofPortFlag)
	pprofAddress := fmt.Sprintf("%s:%d", pprofHost, pprofPort)

	if diagAddress == metricsAddress {
		diagMux = SetupDiagnosticsEndpoint(metricsMux, diagAddress)
	} else if diagAddress == pprofAddress && pprofMux != nil {
		diagMux = SetupDiagnosticsEndpoint(pprofMux, diagAddress)
	} else {
		diagMux = SetupDiagnosticsEndpoint(nil, diagAddress)
	}

	speedTest := ctx.Bool(diagnoticsSpeedTestFlag)
	diagnostic, err := diaglib.NewDiagnosticClient(ctx.Context, diagMux, node.Backend().DataDir(), speedTest)
	if err == nil {
		diagnostic.Setup()
		SetupEndpoints(ctx, node, diagMux, diagnostic)
	} else {
		log.Error("[Diagnostics] Failure in setting up diagnostics", "err", err)
	}
}

func SetupDiagnosticsEndpoint(metricsMux *http.ServeMux, addres string) *http.ServeMux {
	diagMux := http.NewServeMux()

	if metricsMux != nil {
		SetupMiddleMuxHandler(diagMux, metricsMux, "/debug/diag")
	} else {
		middleMux := http.NewServeMux()
		SetupMiddleMuxHandler(diagMux, middleMux, "/debug/diag")

		diagServer := &http.Server{
			Addr:    addres,
			Handler: middleMux,
		}

		go func() {
			if err := diagServer.ListenAndServe(); err != nil {
				log.Error("[Diagnostics] Failure in running diagnostics server", "err", err)
			}
		}()

	}

	return diagMux
}

func SetupMiddleMuxHandler(mux *http.ServeMux, middleMux *http.ServeMux, path string) {
	middleMux.HandleFunc(path+"/", func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, path)
		r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, path)
		mux.ServeHTTP(w, r)
	})
}

func SetupEndpoints(ctx *cli.Context, node *node.ErigonNode, diagMux *http.ServeMux, diagnostic *diaglib.DiagnosticClient) {
	SetupLogsAccess(ctx, diagMux)
	SetupDbAccess(ctx, diagMux)
	SetupCmdLineAccess(diagMux)
	SetupFlagsAccess(ctx, diagMux)
	SetupVersionAccess(diagMux)
	SetupBlockBodyDownload(diagMux)
	SetupHeaderDownloadStats(diagMux)
	SetupNodeInfoAccess(diagMux, node)
	SetupPeersAccess(ctx, diagMux, node, diagnostic)
	SetupBootnodesAccess(diagMux, node)
	SetupStagesAccess(diagMux, diagnostic)
	SetupMemAccess(diagMux)
	SetupHeadersAccess(diagMux, diagnostic)
	SetupBodiesAccess(diagMux, diagnostic)
}
