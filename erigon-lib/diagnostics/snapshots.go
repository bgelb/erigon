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
	"context"
	"fmt"
	"time"

	"github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/log/v3"
)

var (
	SnapshotDownloadStatisticsKey = []byte("diagSnapshotDownloadStatistics")
	SnapshotIndexingStatisticsKey = []byte("diagSnapshotIndexingStatistics")
	SnapshotFillDBStatisticsKey   = []byte("diagSnapshotFillDBStatistics")
)

func (d *DiagnosticClient) setupSnapshotDiagnostics(rootCtx context.Context) {
	d.runSnapshotListener(rootCtx)
	d.runSegmentDownloadingListener(rootCtx)
	d.runSegmentIndexingListener(rootCtx)
	d.runSegmentIndexingFinishedListener(rootCtx)
	d.runSnapshotFilesListListener(rootCtx)
	d.runFileDownloadedListener(rootCtx)
	d.runFillDBListener(rootCtx)
}

func (d *DiagnosticClient) runSnapshotListener(rootCtx context.Context) {
	go func() {
		ctx, ch, closeChannel := Context[SnapshotDownloadStatistics](rootCtx, 1)
		defer closeChannel()

		StartProviders(ctx, TypeOf(SnapshotDownloadStatistics{}), log.Root())
		for {
			select {
			case <-rootCtx.Done():
				return
			case info := <-ch:

				d.mu.Lock()
				d.syncStats.SnapshotDownload.Downloaded = info.Downloaded
				d.syncStats.SnapshotDownload.Total = info.Total
				d.syncStats.SnapshotDownload.TotalTime = info.TotalTime
				d.syncStats.SnapshotDownload.DownloadRate = info.DownloadRate
				d.syncStats.SnapshotDownload.UploadRate = info.UploadRate
				d.syncStats.SnapshotDownload.Peers = info.Peers
				d.syncStats.SnapshotDownload.Files = info.Files
				d.syncStats.SnapshotDownload.Connections = info.Connections
				d.syncStats.SnapshotDownload.Alloc = info.Alloc
				d.syncStats.SnapshotDownload.Sys = info.Sys
				d.syncStats.SnapshotDownload.DownloadFinished = info.DownloadFinished
				d.syncStats.SnapshotDownload.TorrentMetadataReady = info.TorrentMetadataReady
				d.mu.Unlock()

				downloadedPercent := GetShanpshotsPercentDownloaded(info.Downloaded, info.Total, info.TorrentMetadataReady, info.Files)
				remainingBytes := info.Total - info.Downloaded
				downloadTimeLeft := CalculateTime(remainingBytes, info.DownloadRate)
				totalDownloadTimeString := time.Duration(info.TotalTime) * time.Second

				d.updateSnapshotStageStats(SyncStageStats{
					TimeElapsed: totalDownloadTimeString.String(),
					TimeLeft:    downloadTimeLeft,
					Progress:    downloadedPercent,
				}, "Downloading snapshots")

				if info.DownloadFinished {
					d.SaveData()
					return
				}
			}
		}
	}()
}

func GetShanpshotsPercentDownloaded(downloaded uint64, total uint64, torrentMetadataReady int32, files int32) string {
	if torrentMetadataReady < files {
		return "calculating..."
	}

	percent := float32(downloaded) / float32(total/100)

	if percent > 100 {
		percent = 100
	}

	return fmt.Sprintf("%.2f%%", percent)
}

func (d *DiagnosticClient) updateSnapshotStageStats(stats SyncStageStats, subStageInfo string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	idxs := d.GetCurrentSyncIdxs()
	if idxs.Stage == -1 || idxs.SubStage == -1 {
		log.Debug("[Diagnostics] Can't find running stage or substage while updating Snapshots stage stats.", "stages:", d.syncStages, "stats:", stats, "subStageInfo:", subStageInfo)
		return
	}

	d.syncStages[idxs.Stage].SubStages[idxs.SubStage].Stats = stats
}

func (d *DiagnosticClient) runSegmentDownloadingListener(rootCtx context.Context) {
	go func() {
		ctx, ch, closeChannel := Context[SegmentDownloadStatistics](rootCtx, 1)
		defer closeChannel()

		StartProviders(ctx, TypeOf(SegmentDownloadStatistics{}), log.Root())
		for {
			select {
			case <-rootCtx.Done():
				return
			case info := <-ch:
				d.mu.Lock()
				if d.syncStats.SnapshotDownload.SegmentsDownloading == nil {
					d.syncStats.SnapshotDownload.SegmentsDownloading = map[string]SegmentDownloadStatistics{}
				}

				if val, ok := d.syncStats.SnapshotDownload.SegmentsDownloading[info.Name]; ok {
					val.TotalBytes = info.TotalBytes
					val.DownloadedBytes = info.DownloadedBytes
					val.Webseeds = info.Webseeds
					val.Peers = info.Peers

					d.syncStats.SnapshotDownload.SegmentsDownloading[info.Name] = val
				} else {
					d.syncStats.SnapshotDownload.SegmentsDownloading[info.Name] = info
				}
				d.mu.Unlock()
			}
		}
	}()
}

func (d *DiagnosticClient) runSegmentIndexingListener(rootCtx context.Context) {
	go func() {
		ctx, ch, closeChannel := Context[SnapshotIndexingStatistics](rootCtx, 1)
		defer closeChannel()

		StartProviders(ctx, TypeOf(SnapshotIndexingStatistics{}), log.Root())
		for {
			select {
			case <-rootCtx.Done():
				return
			case info := <-ch:
				d.addOrUpdateSegmentIndexingState(info)
				d.updateIndexingStatus()

				if d.syncStats.SnapshotIndexing.IndexingFinished {
					d.SaveData()
					return
				}
			}
		}
	}()
}

func (d *DiagnosticClient) runSegmentIndexingFinishedListener(rootCtx context.Context) {
	go func() {
		ctx, ch, closeChannel := Context[SnapshotSegmentIndexingFinishedUpdate](rootCtx, 1)
		defer closeChannel()

		StartProviders(ctx, TypeOf(SnapshotSegmentIndexingFinishedUpdate{}), log.Root())
		for {
			select {
			case <-rootCtx.Done():
				return
			case info := <-ch:
				d.mu.Lock()
				found := false
				for i := range d.syncStats.SnapshotIndexing.Segments {
					if d.syncStats.SnapshotIndexing.Segments[i].SegmentName == info.SegmentName {
						found = true
						d.syncStats.SnapshotIndexing.Segments[i].Percent = 100
					}
				}

				if !found {
					d.syncStats.SnapshotIndexing.Segments = append(d.syncStats.SnapshotIndexing.Segments, SnapshotSegmentIndexingStatistics{
						SegmentName: info.SegmentName,
						Percent:     100,
						Alloc:       0,
						Sys:         0,
					})
				}

				d.mu.Unlock()

				d.updateIndexingStatus()
			}
		}
	}()
}

func (d *DiagnosticClient) updateIndexingStatus() {
	totalProgressPercent := 0
	for _, seg := range d.syncStats.SnapshotIndexing.Segments {
		totalProgressPercent += seg.Percent
	}

	totalProgress := totalProgressPercent / len(d.syncStats.SnapshotIndexing.Segments)

	d.updateSnapshotStageStats(SyncStageStats{
		TimeElapsed: SecondsToHHMMString(uint64(d.syncStats.SnapshotIndexing.TimeElapsed)),
		TimeLeft:    "unknown",
		Progress:    fmt.Sprintf("%d%%", totalProgress),
	}, "Indexing snapshots")

	if totalProgress >= 100 {
		d.syncStats.SnapshotIndexing.IndexingFinished = true
	}
}

func (d *DiagnosticClient) addOrUpdateSegmentIndexingState(upd SnapshotIndexingStatistics) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.syncStats.SnapshotIndexing.Segments == nil {
		d.syncStats.SnapshotIndexing.Segments = []SnapshotSegmentIndexingStatistics{}
	}

	for i := range upd.Segments {
		found := false
		for j := range d.syncStats.SnapshotIndexing.Segments {
			if d.syncStats.SnapshotIndexing.Segments[j].SegmentName == upd.Segments[i].SegmentName {
				d.syncStats.SnapshotIndexing.Segments[j].Percent = upd.Segments[i].Percent
				d.syncStats.SnapshotIndexing.Segments[j].Alloc = upd.Segments[i].Alloc
				d.syncStats.SnapshotIndexing.Segments[j].Sys = upd.Segments[i].Sys
				found = true
				break
			}
		}

		if !found {
			d.syncStats.SnapshotIndexing.Segments = append(d.syncStats.SnapshotIndexing.Segments, upd.Segments[i])
		}
	}

	d.syncStats.SnapshotIndexing.TimeElapsed = upd.TimeElapsed
}

func (d *DiagnosticClient) runSnapshotFilesListListener(rootCtx context.Context) {
	go func() {
		ctx, ch, closeChannel := Context[SnapshoFilesList](rootCtx, 1)
		defer closeChannel()

		StartProviders(ctx, TypeOf(SnapshoFilesList{}), log.Root())
		for {
			select {
			case <-rootCtx.Done():
				return
			case info := <-ch:
				d.mu.Lock()
				d.snapshotFileList = info
				d.mu.Unlock()

				if len(info.Files) > 0 {
					return
				}
			}
		}
	}()
}

func (d *DiagnosticClient) runFileDownloadedListener(rootCtx context.Context) {
	go func() {
		ctx, ch, closeChannel := Context[FileDownloadedStatisticsUpdate](rootCtx, 1)
		defer closeChannel()

		StartProviders(ctx, TypeOf(FileDownloadedStatisticsUpdate{}), log.Root())
		for {
			select {
			case <-rootCtx.Done():
				return
			case info := <-ch:
				d.mu.Lock()

				if d.syncStats.SnapshotDownload.SegmentsDownloading == nil {
					d.syncStats.SnapshotDownload.SegmentsDownloading = map[string]SegmentDownloadStatistics{}
				}

				if val, ok := d.syncStats.SnapshotDownload.SegmentsDownloading[info.FileName]; ok {
					val.DownloadedStats = FileDownloadedStatistics{
						TimeTook:    info.TimeTook,
						AverageRate: info.AverageRate,
					}

					d.syncStats.SnapshotDownload.SegmentsDownloading[info.FileName] = val
				} else {
					d.syncStats.SnapshotDownload.SegmentsDownloading[info.FileName] = SegmentDownloadStatistics{
						Name:            info.FileName,
						TotalBytes:      0,
						DownloadedBytes: 0,
						Webseeds:        nil,
						Peers:           nil,
						DownloadedStats: FileDownloadedStatistics{
							TimeTook:    info.TimeTook,
							AverageRate: info.AverageRate,
						},
					}
				}

				d.mu.Unlock()
			}
		}
	}()
}

func (d *DiagnosticClient) UpdateFileDownloadedStatistics(downloadedInfo *FileDownloadedStatisticsUpdate, downloadingInfo *SegmentDownloadStatistics) {
	if d.syncStats.SnapshotDownload.SegmentsDownloading == nil {
		d.syncStats.SnapshotDownload.SegmentsDownloading = map[string]SegmentDownloadStatistics{}
	}

	if downloadedInfo != nil {
		dwStats := FileDownloadedStatistics{
			TimeTook:    downloadedInfo.TimeTook,
			AverageRate: downloadedInfo.AverageRate,
		}
		if val, ok := d.syncStats.SnapshotDownload.SegmentsDownloading[downloadedInfo.FileName]; ok {
			val.DownloadedStats = dwStats

			d.syncStats.SnapshotDownload.SegmentsDownloading[downloadedInfo.FileName] = val
		} else {
			d.syncStats.SnapshotDownload.SegmentsDownloading[downloadedInfo.FileName] = SegmentDownloadStatistics{
				Name:            downloadedInfo.FileName,
				TotalBytes:      0,
				DownloadedBytes: 0,
				Webseeds:        make([]SegmentPeer, 0),
				Peers:           make([]SegmentPeer, 0),
				DownloadedStats: dwStats,
			}
		}
	} else {
		if val, ok := d.syncStats.SnapshotDownload.SegmentsDownloading[downloadingInfo.Name]; ok {
			val.TotalBytes = downloadingInfo.TotalBytes
			val.DownloadedBytes = downloadingInfo.DownloadedBytes
			val.Webseeds = downloadingInfo.Webseeds
			val.Peers = downloadingInfo.Peers

			d.syncStats.SnapshotDownload.SegmentsDownloading[downloadingInfo.Name] = val
		} else {
			d.syncStats.SnapshotDownload.SegmentsDownloading[downloadingInfo.Name] = *downloadingInfo
		}
	}
}

func (d *DiagnosticClient) runFillDBListener(rootCtx context.Context) {
	go func() {
		ctx, ch, closeChannel := Context[SnapshotFillDBStageUpdate](rootCtx, 1)
		defer closeChannel()

		StartProviders(ctx, TypeOf(SnapshotFillDBStageUpdate{}), log.Root())
		for {
			select {
			case <-rootCtx.Done():
				return
			case info := <-ch:
				d.SetFillDBInfo(info.Stage)

				totalTimeString := time.Duration(info.TimeElapsed) * time.Second

				d.mu.Lock()
				d.updateSnapshotStageStats(SyncStageStats{
					TimeElapsed: totalTimeString.String(),
					TimeLeft:    "unknown",
					Progress:    fmt.Sprintf("%d%%", (info.Stage.Current*100)/info.Stage.Total),
				}, "Fill DB from snapshots")

				err := d.db.Update(d.ctx, func(tx kv.RwTx) error {
					err := SnapshotFillDBUpdater(d.syncStats.SnapshotFillDB)(tx)
					if err != nil {
						return err
					}

					err = StagesListUpdater(d.syncStages)(tx)
					if err != nil {
						return err
					}

					return nil
				})

				if err != nil {
					log.Warn("[Diagnostics] Failed to update snapshot download info", "err", err)
				}
				d.mu.Unlock()
			}
		}
	}()
}

func (d *DiagnosticClient) SetFillDBInfo(info SnapshotFillDBStage) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.syncStats.SnapshotFillDB.Stages == nil {
		d.syncStats.SnapshotFillDB.Stages = []SnapshotFillDBStage{info}
	} else {

		for idx, stg := range d.syncStats.SnapshotFillDB.Stages {
			if stg.StageName == info.StageName {
				d.syncStats.SnapshotFillDB.Stages[idx] = info
				break
			}
		}
	}
}

func (d *DiagnosticClient) SyncStatistics() SyncStatistics {
	return d.syncStats
}

func (d *DiagnosticClient) SnapshotFilesList() SnapshoFilesList {
	return d.snapshotFileList
}

func SnapshotDownloadInfoFromTx(tx kv.Tx) ([]byte, error) {
	bytes, err := ReadDataFromTable(tx, kv.DiagSyncStages, SnapshotDownloadStatisticsKey)
	if err != nil {
		return nil, err
	}

	return common.CopyBytes(bytes), nil
}

func SnapshotIndexingInfoFromTx(tx kv.Tx) ([]byte, error) {
	bytes, err := ReadDataFromTable(tx, kv.DiagSyncStages, SnapshotIndexingStatisticsKey)
	if err != nil {
		return nil, err
	}

	return common.CopyBytes(bytes), nil
}

func SnapshotFillDBInfoFromTx(tx kv.Tx) ([]byte, error) {
	bytes, err := ReadDataFromTable(tx, kv.DiagSyncStages, SnapshotFillDBStatisticsKey)
	if err != nil {
		return nil, err
	}

	return common.CopyBytes(bytes), nil
}

func SnapshotDownloadUpdater(info SnapshotDownloadStatistics) func(tx kv.RwTx) error {
	return PutDataToTable(kv.DiagSyncStages, SnapshotDownloadStatisticsKey, info)
}

func SnapshotIndexingUpdater(info SnapshotIndexingStatistics) func(tx kv.RwTx) error {
	return PutDataToTable(kv.DiagSyncStages, SnapshotIndexingStatisticsKey, info)
}

func SnapshotFillDBUpdater(info SnapshotFillDBStatistics) func(tx kv.RwTx) error {
	return PutDataToTable(kv.DiagSyncStages, SnapshotFillDBStatisticsKey, info)
}
