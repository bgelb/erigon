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

package sync

import (
	"context"

	"github.com/ledgerwatch/erigon-lib/log/v3"

	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/eth/protocols/eth"
	"github.com/ledgerwatch/erigon/polygon/heimdall"
	"github.com/ledgerwatch/erigon/polygon/p2p"
)

const EventTypeNewBlock = "new-block"
const EventTypeNewBlockHashes = "new-block-hashes"
const EventTypeNewMilestone = "new-milestone"
const EventTypeNewSpan = "new-span"

type EventNewBlock struct {
	NewBlock *types.Block
	PeerId   *p2p.PeerId
}

type EventNewBlockHashes struct {
	NewBlockHashes eth.NewBlockHashesPacket
	PeerId         *p2p.PeerId
}

type EventNewMilestone = *heimdall.Milestone

type EventNewSpan = *heimdall.Span

type Event struct {
	Type string

	newBlock       EventNewBlock
	newBlockHashes EventNewBlockHashes
	newMilestone   EventNewMilestone
	newSpan        EventNewSpan
}

func (e Event) AsNewBlock() EventNewBlock {
	if e.Type != EventTypeNewBlock {
		panic("Event type mismatch")
	}
	return e.newBlock
}

func (e Event) AsNewBlockHashes() EventNewBlockHashes {
	if e.Type != EventTypeNewBlockHashes {
		panic("Event type mismatch")
	}
	return e.newBlockHashes
}

func (e Event) AsNewMilestone() EventNewMilestone {
	if e.Type != EventTypeNewMilestone {
		panic("Event type mismatch")
	}
	return e.newMilestone
}

func (e Event) AsNewSpan() EventNewSpan {
	if e.Type != EventTypeNewSpan {
		panic("Event type mismatch")
	}
	return e.newSpan
}

type TipEvents struct {
	logger          log.Logger
	events          *EventChannel[Event]
	p2pService      p2p.Service
	heimdallService heimdall.Heimdall
}

func NewTipEvents(
	logger log.Logger,
	p2pService p2p.Service,
	heimdallService heimdall.Heimdall,
) *TipEvents {
	eventsCapacity := uint(1000) // more than 3 milestones

	return &TipEvents{
		logger:          logger,
		events:          NewEventChannel[Event](eventsCapacity),
		p2pService:      p2pService,
		heimdallService: heimdallService,
	}
}

func (te *TipEvents) Events() <-chan Event {
	return te.events.Events()
}

func (te *TipEvents) Run(ctx context.Context) error {
	te.logger.Debug(syncLogPrefix("running tip events component"))

	newBlockObserverCancel := te.p2pService.RegisterNewBlockObserver(func(message *p2p.DecodedInboundMessage[*eth.NewBlockPacket]) {
		te.events.PushEvent(Event{
			Type: EventTypeNewBlock,
			newBlock: EventNewBlock{
				NewBlock: message.Decoded.Block,
				PeerId:   message.PeerId,
			},
		})
	})
	defer newBlockObserverCancel()

	newBlockHashesObserverCancel := te.p2pService.RegisterNewBlockHashesObserver(func(message *p2p.DecodedInboundMessage[*eth.NewBlockHashesPacket]) {
		te.events.PushEvent(Event{
			Type: EventTypeNewBlockHashes,
			newBlockHashes: EventNewBlockHashes{
				NewBlockHashes: *message.Decoded,
				PeerId:         message.PeerId,
			},
		})
	})
	defer newBlockHashesObserverCancel()

	milestoneObserverCancel := te.heimdallService.RegisterMilestoneObserver(func(milestone *heimdall.Milestone) {
		te.events.PushEvent(Event{
			Type:         EventTypeNewMilestone,
			newMilestone: milestone,
		})
	})
	defer milestoneObserverCancel()

	spanObserverCancel := te.heimdallService.RegisterSpanObserver(func(span *heimdall.Span) {
		te.events.PushEvent(Event{
			Type:    EventTypeNewSpan,
			newSpan: span,
		})
	})
	defer spanObserverCancel()

	return te.events.Run(ctx)
}
