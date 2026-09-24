package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// Files of conversations, option D (decided by the PM): a file goes peer to
// peer to the devices that are online (WebRTC data channel, DTLS-encrypted,
// on top of the file's own end-to-end encryption); only the devices that
// could not get it that way are served by a copy on the Identity service,
// deleted as soon as each of them has it. Signalling (offer, fetch, answer)
// travels as Olm messages. File transfers never use the TURN relay: the
// server copy is their fallback.
//
// Each device keeps the ciphertext of the files it has in <profile>.files/,
// and serves it to members of the conversation who ask (while dm-listen runs):
// that is how a new device, or someone who missed a large file, gets it later.

type fileSignal struct {
	Action     string    `json:"action"` // offer | fetch | answer
	TransferID string    `json:"transfer_id,omitempty"`
	ConvID     string    `json:"conv_id"`
	FileID     string    `json:"file_id"`
	Size       int64     `json:"size,omitempty"`
	SDP        string    `json:"sdp,omitempty"`
	FromUser   string    `json:"from_user,omitempty"`
	FromDevice string    `json:"from_device,omitempty"`
	At         time.Time `json:"at,omitempty"`
}

const (
	fileChunk        = 16 << 10
	fileOfferWindow  = 10 * time.Second // how long a sender waits for online devices
	fileFetchTimeout = 30 * time.Second // how long a requester waits for a holder to answer
)

var fileIDRe = regexp.MustCompile(`^[a-z0-9]{8,64}$`)

func (c *cli) filesDir() string { return strings.TrimSuffix(c.path, ".json") + ".files" }

func (c *cli) localFile(id string) (string, bool) {
	if !fileIDRe.MatchString(id) {
		return "", false
	}
	return filepath.Join(c.filesDir(), id), true
}

func (c *cli) haveFile(id string) bool {
	p, ok := c.localFile(id)
	if !ok {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func (c *cli) storeFile(id string, ct []byte) error {
	p, ok := c.localFile(id)
	if !ok {
		return errors.New("identifiant de fichier invalide")
	}
	if err := os.MkdirAll(c.filesDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, ct, 0o600)
}

// takeFileSignals removes and returns the pending signals matching keep.
func (e *e2e) takeFileSignals(keep func(fileSignal) bool) []fileSignal {
	var out, rest []fileSignal
	for _, s := range e.st.FileSignals {
		switch {
		case time.Since(s.At) > callSignalTTL:
		case keep(s):
			out = append(out, s)
		default:
			rest = append(rest, s)
		}
	}
	e.st.FileSignals = rest
	return out
}

// newDataPeer opens a peer connection for a file: STUN only, never the relay.
func (c *cli) newDataPeer() (*webrtc.PeerConnection, error) {
	var servers struct {
		ICEServers []struct {
			URLs []string `json:"urls"`
		} `json:"ice_servers"`
	}
	if err := c.do("GET", "/v1/calls/ice-servers", nil, &servers); err != nil {
		return nil, err
	}
	cfg := webrtc.Configuration{}
	for _, s := range servers.ICEServers {
		if len(s.URLs) > 0 && strings.HasPrefix(s.URLs[0], "stun:") {
			cfg.ICEServers = append(cfg.ICEServers, webrtc.ICEServer{URLs: s.URLs})
		}
	}
	return webrtc.NewPeerConnection(cfg)
}

func gather(pc *webrtc.PeerConnection, desc webrtc.SessionDescription) (string, error) {
	done := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(desc); err != nil {
		return "", err
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		return "", errors.New("collecte des chemins réseau trop longue")
	}
	return pc.LocalDescription().SDP, nil
}

// fetchP2P asks holders for a file's ciphertext and receives it from the
// first one that answers.
func (c *cli) fetchP2P(convID, fileID string, holders []deviceInfo, timeout time.Duration) ([]byte, error) {
	if len(holders) == 0 {
		return nil, errors.New("aucun appareil ne peut fournir ce fichier")
	}
	pc, err := c.newDataPeer()
	if err != nil {
		return nil, err
	}
	defer pc.Close()
	dc, err := pc.CreateDataChannel("quarel-file", nil)
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	var buf []byte
	var size int64 = -1
	var failure error
	done := make(chan struct{})
	var once sync.Once
	finish := func(err error) { once.Do(func() { failure = err; close(done) }) }
	dc.OnMessage(func(m webrtc.DataChannelMessage) {
		mu.Lock()
		defer mu.Unlock()
		if m.IsString {
			var h struct {
				Size  int64  `json:"size"`
				Error string `json:"error"`
			}
			json.Unmarshal(m.Data, &h)
			if h.Error != "" {
				finish(errors.New(h.Error))
				return
			}
			size, buf = h.Size, make([]byte, 0, h.Size)
		} else {
			buf = append(buf, m.Data...)
		}
		if size >= 0 && int64(len(buf)) >= size {
			finish(nil)
		}
	})
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return nil, err
	}
	sdp, err := gather(pc, offer)
	if err != nil {
		return nil, err
	}
	transfer := newCallID()
	if err := c.withE2E(func(e *e2e) error {
		return e.sendSecret(holders, "file", fileSignal{Action: "fetch", TransferID: transfer, ConvID: convID, FileID: fileID, SDP: sdp})
	}); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	answered := false
	for !answered && time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		var answers []fileSignal
		c.withE2E(func(e *e2e) error {
			if e.sync(func(string) {}) == nil {
				answers = e.takeFileSignals(func(s fileSignal) bool { return s.Action == "answer" && s.TransferID == transfer })
			}
			return nil
		})
		if len(answers) > 0 {
			if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: answers[0].SDP}); err != nil {
				return nil, err
			}
			answered = true
		}
	}
	if !answered {
		return nil, errors.New("aucun appareil détenteur n'a répondu (il doit être en ligne)")
	}
	select {
	case <-done:
	case <-time.After(time.Until(deadline) + 2*time.Minute):
		return nil, errors.New("transfert interrompu")
	}
	if failure != nil {
		return nil, failure
	}
	dc.SendText("ok")
	time.Sleep(200 * time.Millisecond) // let the acknowledgement leave
	mu.Lock()
	defer mu.Unlock()
	return buf, nil
}

// serveFetch sends a file this device holds to a member of its conversation.
// It reports whether the requester confirmed reception.
func (c *cli) serveFetch(req fileSignal) bool {
	path, ok := c.localFile(req.FileID)
	if !ok {
		return false
	}
	ct, err := os.ReadFile(path)
	if err != nil {
		return false // another holder may answer
	}
	convs, err := c.conversations()
	if err != nil {
		return false
	}
	member := false
	for _, ci := range convs {
		if ci.ID != req.ConvID {
			continue
		}
		for _, m := range ci.Members {
			member = member || m.ID == req.FromUser
		}
	}
	if !member {
		return false // not someone of this conversation: never serve
	}
	pc, err := c.newDataPeer()
	if err != nil {
		return false
	}
	defer pc.Close()
	confirmed := make(chan struct{})
	var once sync.Once
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			header, _ := json.Marshal(map[string]int64{"size": int64(len(ct))})
			dc.SendText(string(header))
			for off := 0; off < len(ct); off += fileChunk {
				for dc.BufferedAmount() > 1<<20 {
					time.Sleep(5 * time.Millisecond)
				}
				if dc.Send(ct[off:min(off+fileChunk, len(ct))]) != nil {
					return
				}
			}
		})
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			if m.IsString && string(m.Data) == "ok" {
				once.Do(func() { close(confirmed) })
			}
		})
	})
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: req.SDP}); err != nil {
		return false
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return false
	}
	sdp, err := gather(pc, answer)
	if err != nil {
		return false
	}
	if err := c.withE2E(func(e *e2e) error {
		devs, err := e.trusted(req.FromUser)
		if err != nil {
			return err
		}
		for _, d := range devs {
			if d.DeviceID == req.FromDevice {
				return e.sendSecret([]deviceInfo{d}, "file", fileSignal{Action: "answer", TransferID: req.TransferID, ConvID: req.ConvID, FileID: req.FileID, SDP: sdp})
			}
		}
		return errors.New("requesting device not trusted")
	}); err != nil {
		return false
	}
	select {
	case <-confirmed:
		return true
	case <-time.After(3 * time.Minute):
		return false
	}
}

// autoFetch takes a file offered by a device that just sent it.
func (c *cli) autoFetch(offer fileSignal, out func(string)) {
	if c.haveFile(offer.FileID) {
		return
	}
	var holder []deviceInfo
	c.withE2E(func(e *e2e) error {
		devs, err := e.trusted(offer.FromUser)
		for _, d := range devs {
			if d.DeviceID == offer.FromDevice {
				holder = append(holder, d)
			}
		}
		return err
	})
	ct, err := c.fetchP2P(offer.ConvID, offer.FileID, holder, fileOfferWindow)
	if err != nil {
		out("⚠ fichier non reçu en direct (" + err.Error() + ") : il passera par le serveur")
		return
	}
	if err := c.storeFile(offer.FileID, ct); err == nil {
		out(fmt.Sprintf("📎 fichier reçu en direct (%s)", humanSize(int64(len(ct)))))
	}
}

// pullServerCopy fetches a file's ciphertext from the server if this device
// lacks it, then acknowledges it so the server can delete its copy.
func (c *cli) pullServerCopy(convID string, ref *fileRef) error {
	if ref == nil || ref.ServerID == "" {
		return nil
	}
	if !c.haveFile(ref.ID) {
		ct, err := c.downloadServerCopy(convID, ref.ServerID)
		if err != nil {
			return err
		}
		if err := checkFile(convID, ref, ct); err != nil {
			return err
		}
		if err := c.storeFile(ref.ID, ct); err != nil {
			return err
		}
	}
	return c.do("POST", "/v1/dms/"+convID+"/files/"+ref.ServerID+"/ack", nil, nil)
}
