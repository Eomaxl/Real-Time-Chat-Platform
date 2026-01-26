package sfu

import (
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
)

// SFUSession represents an SFU session for a call
type SFUSession struct {
	ID          string
	CallID      string
	ChannelID   string
	CreatedAt   time.Time
	Publishers  map[string]*Publisher
	Subscribers map[string]*Subscriber
	mu          sync.RWMutex
}

// Publisher represents a media publisher in the SFU
type Publisher struct {
	UserID         string
	PeerConnection *webrtc.PeerConnection
	Tracks         []*webrtc.TrackLocalStaticRTP
	CreatedAt      time.Time
	mu             sync.RWMutex
}

// Subscriber represents a media subscriber in the SFU
type Subscriber struct {
	UserID         string
	PeerConnection *webrtc.PeerConnection
	SubscribedTo   map[string]bool // Map of publisher UserIDs
	CreatedAt      time.Time
	mu             sync.RWMutex
}

// TrackInfo represents information about a media track
type TrackInfo struct {
	TrackID   string
	StreamID  string
	Kind      string // "audio" or "video"
	SSRC      uint32
	Publisher string
}

// SFUStats represents statistics for an SFU session
type SFUStats struct {
	SessionID       string
	PublisherCount  int
	SubscriberCount int
	TotalTracks     int
	BytesSent       uint64
	BytesReceived   uint64
	PacketsLost     uint64
	Jitter          float64
}

// BandwidthConfig represents bandwidth management configuration
type BandwidthConfig struct {
	MaxBitrate     uint64 // bits per second
	MinBitrate     uint64
	StartBitrate   uint64
	EnableAdaptive bool
}

// QualityLevel represents video quality levels
type QualityLevel string

const (
	QualityLow    QualityLevel = "low"
	QualityMedium QualityLevel = "medium"
	QualityHigh   QualityLevel = "high"
)

// QualityConfig represents quality control configuration
type QualityConfig struct {
	Level           QualityLevel
	MaxWidth        int
	MaxHeight       int
	MaxFrameRate    int
	EnableSimulcast bool
}

// SFUConfig represents SFU service configuration
type SFUConfig struct {
	ICEServers     []webrtc.ICEServer
	Bandwidth      BandwidthConfig
	Quality        QualityConfig
	MaxPublishers  int
	MaxSubscribers int
	SessionTimeout time.Duration
}

// NewSFUSession creates a new SFU session
func NewSFUSession(callID, channelID string) *SFUSession {
	return &SFUSession{
		ID:          callID,
		CallID:      callID,
		ChannelID:   channelID,
		CreatedAt:   time.Now(),
		Publishers:  make(map[string]*Publisher),
		Subscribers: make(map[string]*Subscriber),
	}
}

// AddPublisher adds a publisher to the session
func (s *SFUSession) AddPublisher(userID string, pc *webrtc.PeerConnection) *Publisher {
	s.mu.Lock()
	defer s.mu.Unlock()

	publisher := &Publisher{
		UserID:         userID,
		PeerConnection: pc,
		Tracks:         make([]*webrtc.TrackLocalStaticRTP, 0),
		CreatedAt:      time.Now(),
	}
	s.Publishers[userID] = publisher
	return publisher
}

// AddSubscriber adds a subscriber to the session
func (s *SFUSession) AddSubscriber(userID string, pc *webrtc.PeerConnection) *Subscriber {
	s.mu.Lock()
	defer s.mu.Unlock()

	subscriber := &Subscriber{
		UserID:         userID,
		PeerConnection: pc,
		SubscribedTo:   make(map[string]bool),
		CreatedAt:      time.Now(),
	}
	s.Subscribers[userID] = subscriber
	return subscriber
}

// RemovePublisher removes a publisher from the session
func (s *SFUSession) RemovePublisher(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Publishers, userID)
}

// RemoveSubscriber removes a subscriber from the session
func (s *SFUSession) RemoveSubscriber(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Subscribers, userID)
}

// GetPublisher returns a publisher by user ID
func (s *SFUSession) GetPublisher(userID string) (*Publisher, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	publisher, ok := s.Publishers[userID]
	return publisher, ok
}

// GetSubscriber returns a subscriber by user ID
func (s *SFUSession) GetSubscriber(userID string) (*Subscriber, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	subscriber, ok := s.Subscribers[userID]
	return subscriber, ok
}

// GetStats returns statistics for the session
func (s *SFUSession) GetStats() *SFUStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &SFUStats{
		SessionID:       s.ID,
		PublisherCount:  len(s.Publishers),
		SubscriberCount: len(s.Subscribers),
	}

	// Count total tracks
	for _, pub := range s.Publishers {
		pub.mu.RLock()
		stats.TotalTracks += len(pub.Tracks)
		pub.mu.RUnlock()
	}

	return stats
}

// AddTrack adds a track to a publisher
func (p *Publisher) AddTrack(track *webrtc.TrackLocalStaticRTP) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Tracks = append(p.Tracks, track)
}

// GetTracks returns all tracks for a publisher
func (p *Publisher) GetTracks() []*webrtc.TrackLocalStaticRTP {
	p.mu.RLock()
	defer p.mu.RUnlock()
	tracks := make([]*webrtc.TrackLocalStaticRTP, len(p.Tracks))
	copy(tracks, p.Tracks)
	return tracks
}

// Subscribe marks a subscriber as subscribed to a publisher
func (s *Subscriber) Subscribe(publisherID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SubscribedTo[publisherID] = true
}

// Unsubscribe marks a subscriber as unsubscribed from a publisher
func (s *Subscriber) Unsubscribe(publisherID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.SubscribedTo, publisherID)
}

// IsSubscribedTo checks if subscriber is subscribed to a publisher
func (s *Subscriber) IsSubscribedTo(publisherID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SubscribedTo[publisherID]
}
