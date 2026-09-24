// Command olminterop is the Go (goolm) side of the client's crypto
// interoperability test (client/crypto/interop.mjs): it answers JSON
// commands, one per line, on stdin/stdout.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/anlekg/quarel/pkg/e2ekeys"
	"maunium.net/go/mautrix/crypto/goolm"
	"maunium.net/go/mautrix/crypto/olm"
	"maunium.net/go/mautrix/id"
)

type cmd struct {
	Op, Text, Body, SenderCurve, SessionKey, Exported, Key, Msg, Sig string
	Type                                                             int
	Index                                                            uint32
}

func main() {
	goolm.Register()
	acc, _ := olm.NewAccount()
	var sess olm.Session
	var out olm.OutboundGroupSession
	var in olm.InboundGroupSession
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var c cmd
		json.Unmarshal(sc.Bytes(), &c)
		res := map[string]any{}
		var err error
		switch c.Op {
		case "init":
			ed, curve, _ := acc.IdentityKeys()
			acc.GenOneTimeKeys(1)
			otks, _ := acc.OneTimeKeys()
			for kid, k := range otks {
				res["otk_id"], res["otk"] = kid, string(k)
			}
			acc.MarkKeysAsPublished()
			sig, _ := acc.Sign([]byte("quarel-interop"))
			res["ed25519"], res["curve25519"], res["signature"] = string(ed), string(curve), string(sig)
		case "verify":
			err = e2ekeys.Verify(c.Key, []byte(c.Msg), c.Sig)
			res["ok"] = err == nil
			err = nil
		case "olm_inbound":
			curve := id.Curve25519(c.SenderCurve)
			if sess, err = acc.NewInboundSessionFrom(&curve, c.Body); err == nil {
				var pt []byte
				if pt, err = sess.Decrypt(c.Body, id.OlmMsgTypePreKey); err == nil {
					res["plaintext"] = string(pt)
					acc.RemoveOneTimeKeys(sess)
				}
			}
		case "olm_encrypt":
			var mt id.OlmMsgType
			var ct []byte
			if mt, ct, err = sess.Encrypt([]byte(c.Text)); err == nil {
				res["type"], res["body"] = int(mt), string(ct)
			}
		case "olm_decrypt":
			var pt []byte
			if pt, err = sess.Decrypt(c.Body, id.OlmMsgType(c.Type)); err == nil {
				res["plaintext"] = string(pt)
			}
		case "megolm_out":
			if out, err = olm.NewOutboundGroupSession(); err == nil {
				res["session_id"], res["session_key"] = string(out.ID()), out.Key()
			}
		case "megolm_encrypt":
			var ct []byte
			if ct, err = out.Encrypt([]byte(c.Text)); err == nil {
				res["body"] = string(ct)
			}
		case "megolm_in":
			if in, err = olm.NewInboundGroupSession([]byte(c.SessionKey)); err == nil {
				res["session_id"] = string(in.ID())
			}
		case "megolm_import":
			if in, err = olm.InboundGroupSessionImport([]byte(c.Exported)); err == nil {
				res["session_id"] = string(in.ID())
			}
		case "megolm_export":
			var exp []byte
			if exp, err = in.Export(c.Index); err == nil {
				res["exported"] = string(exp)
			}
		case "megolm_decrypt":
			var pt []byte
			var idx uint
			if pt, idx, err = in.Decrypt([]byte(c.Body)); err == nil {
				res["plaintext"], res["index"] = string(pt), idx
			}
		default:
			err = fmt.Errorf("unknown op %q", c.Op)
		}
		if err != nil {
			res["error"] = err.Error()
		}
		enc.Encode(res)
	}
}
