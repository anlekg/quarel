//! Olm and Megolm for the Quarel client, over vodozemac. Everything uses the
//! "version 1" formats (libolm's), so messages interoperate with the Go test
//! client (goolm). Keys, signatures and messages are unpadded base64 strings;
//! pickles are encrypted with a 32-byte key and stay on the device.

use std::collections::BTreeMap;
use vodozemac::megolm::{
    ExportedSessionKey, GroupSession, GroupSessionPickle, InboundGroupSession, InboundGroupSessionPickle,
    MegolmMessage, SessionConfig as MegolmConfig, SessionKey,
};
use vodozemac::olm::{Account, AccountPickle, OlmMessage, Session, SessionConfig, SessionPickle};
use vodozemac::{base64_decode, base64_encode, Curve25519PublicKey};
use wasm_bindgen::prelude::*;

fn err<E: std::fmt::Display>(e: E) -> JsError {
    JsError::new(&e.to_string())
}

fn pickle_key(key: &[u8]) -> Result<[u8; 32], JsError> {
    key.try_into().map_err(|_| JsError::new("pickle key must be 32 bytes"))
}

#[wasm_bindgen]
pub struct OlmAccount(Account);

#[wasm_bindgen]
impl OlmAccount {
    #[wasm_bindgen(constructor)]
    pub fn new() -> OlmAccount {
        OlmAccount(Account::new())
    }

    #[wasm_bindgen(js_name = fromPickle)]
    pub fn from_pickle(pickle: &str, key: &[u8]) -> Result<OlmAccount, JsError> {
        let p = AccountPickle::from_encrypted(pickle, &pickle_key(key)?).map_err(err)?;
        Ok(OlmAccount(Account::from_pickle(p)))
    }

    pub fn pickle(&self, key: &[u8]) -> Result<String, JsError> {
        Ok(self.0.pickle().encrypt(&pickle_key(key)?))
    }

    #[wasm_bindgen(getter)]
    pub fn ed25519(&self) -> String {
        self.0.ed25519_key().to_base64()
    }

    #[wasm_bindgen(getter)]
    pub fn curve25519(&self) -> String {
        self.0.curve25519_key().to_base64()
    }

    /// Ed25519 signature (base64) of message by the device key.
    pub fn sign(&self, message: &[u8]) -> String {
        self.0.sign(message).to_base64()
    }

    #[wasm_bindgen(js_name = generateOneTimeKeys)]
    pub fn generate_one_time_keys(&mut self, count: usize) {
        self.0.generate_one_time_keys(count);
    }

    /// Unpublished one-time keys, as JSON {key id: key}.
    #[wasm_bindgen(js_name = oneTimeKeys)]
    pub fn one_time_keys(&self) -> String {
        let keys: BTreeMap<String, String> =
            self.0.one_time_keys().into_iter().map(|(id, k)| (id.to_base64(), k.to_base64())).collect();
        serde_json::to_string(&keys).unwrap_or_default()
    }

    #[wasm_bindgen(js_name = markKeysAsPublished)]
    pub fn mark_keys_as_published(&mut self) {
        self.0.mark_keys_as_published();
    }

    #[wasm_bindgen(js_name = outboundSession)]
    pub fn outbound_session(&self, identity_key: &str, one_time_key: &str) -> Result<OlmSession, JsError> {
        let ik = Curve25519PublicKey::from_base64(identity_key).map_err(err)?;
        let otk = Curve25519PublicKey::from_base64(one_time_key).map_err(err)?;
        Ok(OlmSession(self.0.create_outbound_session(SessionConfig::version_1(), ik, otk).map_err(err)?))
    }

    /// Opens a session from a pre-key message (type 0) and decrypts it; the
    /// one-time key it used is removed.
    #[wasm_bindgen(js_name = inboundSession)]
    pub fn inbound_session(&mut self, identity_key: &str, body: &str) -> Result<InboundResult, JsError> {
        let ik = Curve25519PublicKey::from_base64(identity_key).map_err(err)?;
        let msg = match OlmMessage::from_parts(0, &base64_decode(body).map_err(err)?).map_err(err)? {
            OlmMessage::PreKey(m) => m,
            _ => return Err(JsError::new("not a pre-key message")),
        };
        let r = self.0.create_inbound_session(SessionConfig::version_1(), ik, &msg).map_err(err)?;
        let plaintext = String::from_utf8(r.plaintext).map_err(err)?;
        Ok(InboundResult { session: Some(OlmSession(r.session)), plaintext })
    }
}

impl Default for OlmAccount {
    fn default() -> Self {
        Self::new()
    }
}

#[wasm_bindgen]
pub struct InboundResult {
    session: Option<OlmSession>,
    plaintext: String,
}

#[wasm_bindgen]
impl InboundResult {
    /// The new session (can be taken once).
    #[wasm_bindgen(js_name = takeSession)]
    pub fn take_session(&mut self) -> Option<OlmSession> {
        self.session.take()
    }

    #[wasm_bindgen(getter)]
    pub fn plaintext(&self) -> String {
        self.plaintext.clone()
    }
}

#[wasm_bindgen]
pub struct OlmSession(Session);

#[wasm_bindgen]
impl OlmSession {
    #[wasm_bindgen(js_name = fromPickle)]
    pub fn from_pickle(pickle: &str, key: &[u8]) -> Result<OlmSession, JsError> {
        let p = SessionPickle::from_encrypted(pickle, &pickle_key(key)?).map_err(err)?;
        Ok(OlmSession(Session::from_pickle(p)))
    }

    pub fn pickle(&self, key: &[u8]) -> Result<String, JsError> {
        Ok(self.0.pickle().encrypt(&pickle_key(key)?))
    }

    #[wasm_bindgen(getter, js_name = sessionId)]
    pub fn session_id(&self) -> String {
        self.0.session_id()
    }

    /// Encrypts; returns JSON {"type": 0|1, "body": "…"}.
    pub fn encrypt(&mut self, plaintext: &str) -> Result<String, JsError> {
        let (t, body) = self.0.encrypt(plaintext).map_err(err)?.to_parts();
        Ok(serde_json::json!({ "type": t, "body": base64_encode(body) }).to_string())
    }

    pub fn decrypt(&mut self, message_type: usize, body: &str) -> Result<String, JsError> {
        let m = OlmMessage::from_parts(message_type, &base64_decode(body).map_err(err)?).map_err(err)?;
        String::from_utf8(self.0.decrypt(&m).map_err(err)?).map_err(err)
    }
}

#[wasm_bindgen]
pub struct MegolmOutbound(GroupSession);

#[wasm_bindgen]
impl MegolmOutbound {
    #[wasm_bindgen(constructor)]
    pub fn new() -> MegolmOutbound {
        MegolmOutbound(GroupSession::new(MegolmConfig::version_1()))
    }

    #[wasm_bindgen(js_name = fromPickle)]
    pub fn from_pickle(pickle: &str, key: &[u8]) -> Result<MegolmOutbound, JsError> {
        let p = GroupSessionPickle::from_encrypted(pickle, &pickle_key(key)?).map_err(err)?;
        Ok(MegolmOutbound(GroupSession::from_pickle(p)))
    }

    pub fn pickle(&self, key: &[u8]) -> Result<String, JsError> {
        Ok(self.0.pickle().encrypt(&pickle_key(key)?))
    }

    #[wasm_bindgen(getter, js_name = sessionId)]
    pub fn session_id(&self) -> String {
        self.0.session_id()
    }

    /// The key to share (at the current index), base64.
    #[wasm_bindgen(getter, js_name = sessionKey)]
    pub fn session_key(&self) -> String {
        self.0.session_key().to_base64()
    }

    pub fn encrypt(&mut self, plaintext: &str) -> String {
        self.0.encrypt(plaintext).to_base64()
    }
}

impl Default for MegolmOutbound {
    fn default() -> Self {
        Self::new()
    }
}

#[wasm_bindgen]
pub struct MegolmInbound(InboundGroupSession);

#[wasm_bindgen]
pub struct Decrypted {
    plaintext: String,
    index: u32,
}

#[wasm_bindgen]
impl Decrypted {
    #[wasm_bindgen(getter)]
    pub fn plaintext(&self) -> String {
        self.plaintext.clone()
    }
    #[wasm_bindgen(getter)]
    pub fn index(&self) -> u32 {
        self.index
    }
}

#[wasm_bindgen]
impl MegolmInbound {
    /// From a shared session key (room_key).
    #[wasm_bindgen(constructor)]
    pub fn new(session_key: &str) -> Result<MegolmInbound, JsError> {
        let k = SessionKey::from_base64(session_key).map_err(err)?;
        Ok(MegolmInbound(InboundGroupSession::new(&k, MegolmConfig::version_1())))
    }

    /// From an exported key (history transfer, backup).
    #[wasm_bindgen(js_name = import)]
    pub fn import(exported: &str) -> Result<MegolmInbound, JsError> {
        let k = ExportedSessionKey::from_base64(exported).map_err(err)?;
        Ok(MegolmInbound(InboundGroupSession::import(&k, MegolmConfig::version_1())))
    }

    #[wasm_bindgen(js_name = fromPickle)]
    pub fn from_pickle(pickle: &str, key: &[u8]) -> Result<MegolmInbound, JsError> {
        let p = InboundGroupSessionPickle::from_encrypted(pickle, &pickle_key(key)?).map_err(err)?;
        Ok(MegolmInbound(InboundGroupSession::from_pickle(p)))
    }

    pub fn pickle(&self, key: &[u8]) -> Result<String, JsError> {
        Ok(self.0.pickle().encrypt(&pickle_key(key)?))
    }

    #[wasm_bindgen(getter, js_name = sessionId)]
    pub fn session_id(&self) -> String {
        self.0.session_id()
    }

    #[wasm_bindgen(getter, js_name = firstKnownIndex)]
    pub fn first_known_index(&self) -> u32 {
        self.0.first_known_index()
    }

    #[wasm_bindgen(js_name = exportAt)]
    pub fn export_at(&mut self, index: u32) -> Option<String> {
        self.0.export_at(index).map(|k| k.to_base64())
    }

    pub fn decrypt(&mut self, ciphertext: &str) -> Result<Decrypted, JsError> {
        let m = MegolmMessage::from_base64(ciphertext).map_err(err)?;
        let d = self.0.decrypt(&m).map_err(err)?;
        Ok(Decrypted { plaintext: String::from_utf8(d.plaintext).map_err(err)?, index: d.message_index })
    }
}
