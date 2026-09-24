/* @ts-self-types="./quarel_crypto.d.ts" */

export class Decrypted {
    static __wrap(ptr) {
        const obj = Object.create(Decrypted.prototype);
        obj.__wbg_ptr = ptr;
        DecryptedFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        DecryptedFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_decrypted_free(ptr, 0);
    }
    /**
     * @returns {number}
     */
    get index() {
        const ret = wasm.decrypted_index(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * @returns {string}
     */
    get plaintext() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.decrypted_plaintext(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
}
if (Symbol.dispose) Decrypted.prototype[Symbol.dispose] = Decrypted.prototype.free;

export class InboundResult {
    static __wrap(ptr) {
        const obj = Object.create(InboundResult.prototype);
        obj.__wbg_ptr = ptr;
        InboundResultFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        InboundResultFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_inboundresult_free(ptr, 0);
    }
    /**
     * @returns {string}
     */
    get plaintext() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.inboundresult_plaintext(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * The new session (can be taken once).
     * @returns {OlmSession | undefined}
     */
    takeSession() {
        const ret = wasm.inboundresult_takeSession(this.__wbg_ptr);
        return ret === 0 ? undefined : OlmSession.__wrap(ret);
    }
}
if (Symbol.dispose) InboundResult.prototype[Symbol.dispose] = InboundResult.prototype.free;

export class MegolmInbound {
    static __wrap(ptr) {
        const obj = Object.create(MegolmInbound.prototype);
        obj.__wbg_ptr = ptr;
        MegolmInboundFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        MegolmInboundFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_megolminbound_free(ptr, 0);
    }
    /**
     * @param {string} ciphertext
     * @returns {Decrypted}
     */
    decrypt(ciphertext) {
        const ptr0 = passStringToWasm0(ciphertext, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.megolminbound_decrypt(this.__wbg_ptr, ptr0, len0);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return Decrypted.__wrap(ret[0]);
    }
    /**
     * @param {number} index
     * @returns {string | undefined}
     */
    exportAt(index) {
        const ret = wasm.megolminbound_exportAt(this.__wbg_ptr, index);
        let v1;
        if (ret[0] !== 0) {
            v1 = getStringFromWasm0(ret[0], ret[1]);
            wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        }
        return v1;
    }
    /**
     * @returns {number}
     */
    get firstKnownIndex() {
        const ret = wasm.megolminbound_firstKnownIndex(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * @param {string} pickle
     * @param {Uint8Array} key
     * @returns {MegolmInbound}
     */
    static fromPickle(pickle, key) {
        const ptr0 = passStringToWasm0(pickle, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ptr1 = passArray8ToWasm0(key, wasm.__wbindgen_malloc);
        const len1 = WASM_VECTOR_LEN;
        const ret = wasm.megolminbound_fromPickle(ptr0, len0, ptr1, len1);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return MegolmInbound.__wrap(ret[0]);
    }
    /**
     * From an exported key (history transfer, backup).
     * @param {string} exported
     * @returns {MegolmInbound}
     */
    static import(exported) {
        const ptr0 = passStringToWasm0(exported, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.megolminbound_import(ptr0, len0);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return MegolmInbound.__wrap(ret[0]);
    }
    /**
     * From a shared session key (room_key).
     * @param {string} session_key
     */
    constructor(session_key) {
        const ptr0 = passStringToWasm0(session_key, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.megolminbound_new(ptr0, len0);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        this.__wbg_ptr = ret[0];
        MegolmInboundFinalization.register(this, this.__wbg_ptr, this);
        return this;
    }
    /**
     * @param {Uint8Array} key
     * @returns {string}
     */
    pickle(key) {
        let deferred3_0;
        let deferred3_1;
        try {
            const ptr0 = passArray8ToWasm0(key, wasm.__wbindgen_malloc);
            const len0 = WASM_VECTOR_LEN;
            const ret = wasm.megolminbound_pickle(this.__wbg_ptr, ptr0, len0);
            var ptr2 = ret[0];
            var len2 = ret[1];
            if (ret[3]) {
                ptr2 = 0; len2 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred3_0 = ptr2;
            deferred3_1 = len2;
            return getStringFromWasm0(ptr2, len2);
        } finally {
            wasm.__wbindgen_free(deferred3_0, deferred3_1, 1);
        }
    }
    /**
     * @returns {string}
     */
    get sessionId() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.megolminbound_sessionId(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
}
if (Symbol.dispose) MegolmInbound.prototype[Symbol.dispose] = MegolmInbound.prototype.free;

export class MegolmOutbound {
    static __wrap(ptr) {
        const obj = Object.create(MegolmOutbound.prototype);
        obj.__wbg_ptr = ptr;
        MegolmOutboundFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        MegolmOutboundFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_megolmoutbound_free(ptr, 0);
    }
    /**
     * @param {string} plaintext
     * @returns {string}
     */
    encrypt(plaintext) {
        let deferred2_0;
        let deferred2_1;
        try {
            const ptr0 = passStringToWasm0(plaintext, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
            const len0 = WASM_VECTOR_LEN;
            const ret = wasm.megolmoutbound_encrypt(this.__wbg_ptr, ptr0, len0);
            deferred2_0 = ret[0];
            deferred2_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred2_0, deferred2_1, 1);
        }
    }
    /**
     * @param {string} pickle
     * @param {Uint8Array} key
     * @returns {MegolmOutbound}
     */
    static fromPickle(pickle, key) {
        const ptr0 = passStringToWasm0(pickle, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ptr1 = passArray8ToWasm0(key, wasm.__wbindgen_malloc);
        const len1 = WASM_VECTOR_LEN;
        const ret = wasm.megolmoutbound_fromPickle(ptr0, len0, ptr1, len1);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return MegolmOutbound.__wrap(ret[0]);
    }
    constructor() {
        const ret = wasm.megolmoutbound_new();
        this.__wbg_ptr = ret;
        MegolmOutboundFinalization.register(this, this.__wbg_ptr, this);
        return this;
    }
    /**
     * @param {Uint8Array} key
     * @returns {string}
     */
    pickle(key) {
        let deferred3_0;
        let deferred3_1;
        try {
            const ptr0 = passArray8ToWasm0(key, wasm.__wbindgen_malloc);
            const len0 = WASM_VECTOR_LEN;
            const ret = wasm.megolmoutbound_pickle(this.__wbg_ptr, ptr0, len0);
            var ptr2 = ret[0];
            var len2 = ret[1];
            if (ret[3]) {
                ptr2 = 0; len2 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred3_0 = ptr2;
            deferred3_1 = len2;
            return getStringFromWasm0(ptr2, len2);
        } finally {
            wasm.__wbindgen_free(deferred3_0, deferred3_1, 1);
        }
    }
    /**
     * @returns {string}
     */
    get sessionId() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.megolmoutbound_sessionId(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * The key to share (at the current index), base64.
     * @returns {string}
     */
    get sessionKey() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.megolmoutbound_sessionKey(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
}
if (Symbol.dispose) MegolmOutbound.prototype[Symbol.dispose] = MegolmOutbound.prototype.free;

export class OlmAccount {
    static __wrap(ptr) {
        const obj = Object.create(OlmAccount.prototype);
        obj.__wbg_ptr = ptr;
        OlmAccountFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        OlmAccountFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_olmaccount_free(ptr, 0);
    }
    /**
     * @returns {string}
     */
    get curve25519() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.olmaccount_curve25519(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @returns {string}
     */
    get ed25519() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.olmaccount_ed25519(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @param {string} pickle
     * @param {Uint8Array} key
     * @returns {OlmAccount}
     */
    static fromPickle(pickle, key) {
        const ptr0 = passStringToWasm0(pickle, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ptr1 = passArray8ToWasm0(key, wasm.__wbindgen_malloc);
        const len1 = WASM_VECTOR_LEN;
        const ret = wasm.olmaccount_fromPickle(ptr0, len0, ptr1, len1);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return OlmAccount.__wrap(ret[0]);
    }
    /**
     * @param {number} count
     */
    generateOneTimeKeys(count) {
        wasm.olmaccount_generateOneTimeKeys(this.__wbg_ptr, count);
    }
    /**
     * Opens a session from a pre-key message (type 0) and decrypts it; the
     * one-time key it used is removed.
     * @param {string} identity_key
     * @param {string} body
     * @returns {InboundResult}
     */
    inboundSession(identity_key, body) {
        const ptr0 = passStringToWasm0(identity_key, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ptr1 = passStringToWasm0(body, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len1 = WASM_VECTOR_LEN;
        const ret = wasm.olmaccount_inboundSession(this.__wbg_ptr, ptr0, len0, ptr1, len1);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return InboundResult.__wrap(ret[0]);
    }
    markKeysAsPublished() {
        wasm.olmaccount_markKeysAsPublished(this.__wbg_ptr);
    }
    constructor() {
        const ret = wasm.olmaccount_new();
        this.__wbg_ptr = ret;
        OlmAccountFinalization.register(this, this.__wbg_ptr, this);
        return this;
    }
    /**
     * Unpublished one-time keys, as JSON {key id: key}.
     * @returns {string}
     */
    oneTimeKeys() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.olmaccount_oneTimeKeys(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * @param {string} identity_key
     * @param {string} one_time_key
     * @returns {OlmSession}
     */
    outboundSession(identity_key, one_time_key) {
        const ptr0 = passStringToWasm0(identity_key, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ptr1 = passStringToWasm0(one_time_key, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len1 = WASM_VECTOR_LEN;
        const ret = wasm.olmaccount_outboundSession(this.__wbg_ptr, ptr0, len0, ptr1, len1);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return OlmSession.__wrap(ret[0]);
    }
    /**
     * @param {Uint8Array} key
     * @returns {string}
     */
    pickle(key) {
        let deferred3_0;
        let deferred3_1;
        try {
            const ptr0 = passArray8ToWasm0(key, wasm.__wbindgen_malloc);
            const len0 = WASM_VECTOR_LEN;
            const ret = wasm.olmaccount_pickle(this.__wbg_ptr, ptr0, len0);
            var ptr2 = ret[0];
            var len2 = ret[1];
            if (ret[3]) {
                ptr2 = 0; len2 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred3_0 = ptr2;
            deferred3_1 = len2;
            return getStringFromWasm0(ptr2, len2);
        } finally {
            wasm.__wbindgen_free(deferred3_0, deferred3_1, 1);
        }
    }
    /**
     * Ed25519 signature (base64) of message by the device key.
     * @param {Uint8Array} message
     * @returns {string}
     */
    sign(message) {
        let deferred2_0;
        let deferred2_1;
        try {
            const ptr0 = passArray8ToWasm0(message, wasm.__wbindgen_malloc);
            const len0 = WASM_VECTOR_LEN;
            const ret = wasm.olmaccount_sign(this.__wbg_ptr, ptr0, len0);
            deferred2_0 = ret[0];
            deferred2_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred2_0, deferred2_1, 1);
        }
    }
}
if (Symbol.dispose) OlmAccount.prototype[Symbol.dispose] = OlmAccount.prototype.free;

export class OlmSession {
    static __wrap(ptr) {
        const obj = Object.create(OlmSession.prototype);
        obj.__wbg_ptr = ptr;
        OlmSessionFinalization.register(obj, obj.__wbg_ptr, obj);
        return obj;
    }
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        OlmSessionFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_olmsession_free(ptr, 0);
    }
    /**
     * @param {number} message_type
     * @param {string} body
     * @returns {string}
     */
    decrypt(message_type, body) {
        let deferred3_0;
        let deferred3_1;
        try {
            const ptr0 = passStringToWasm0(body, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
            const len0 = WASM_VECTOR_LEN;
            const ret = wasm.olmsession_decrypt(this.__wbg_ptr, message_type, ptr0, len0);
            var ptr2 = ret[0];
            var len2 = ret[1];
            if (ret[3]) {
                ptr2 = 0; len2 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred3_0 = ptr2;
            deferred3_1 = len2;
            return getStringFromWasm0(ptr2, len2);
        } finally {
            wasm.__wbindgen_free(deferred3_0, deferred3_1, 1);
        }
    }
    /**
     * Encrypts; returns JSON {"type": 0|1, "body": "…"}.
     * @param {string} plaintext
     * @returns {string}
     */
    encrypt(plaintext) {
        let deferred3_0;
        let deferred3_1;
        try {
            const ptr0 = passStringToWasm0(plaintext, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
            const len0 = WASM_VECTOR_LEN;
            const ret = wasm.olmsession_encrypt(this.__wbg_ptr, ptr0, len0);
            var ptr2 = ret[0];
            var len2 = ret[1];
            if (ret[3]) {
                ptr2 = 0; len2 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred3_0 = ptr2;
            deferred3_1 = len2;
            return getStringFromWasm0(ptr2, len2);
        } finally {
            wasm.__wbindgen_free(deferred3_0, deferred3_1, 1);
        }
    }
    /**
     * @param {string} pickle
     * @param {Uint8Array} key
     * @returns {OlmSession}
     */
    static fromPickle(pickle, key) {
        const ptr0 = passStringToWasm0(pickle, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ptr1 = passArray8ToWasm0(key, wasm.__wbindgen_malloc);
        const len1 = WASM_VECTOR_LEN;
        const ret = wasm.olmsession_fromPickle(ptr0, len0, ptr1, len1);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        return OlmSession.__wrap(ret[0]);
    }
    /**
     * @param {Uint8Array} key
     * @returns {string}
     */
    pickle(key) {
        let deferred3_0;
        let deferred3_1;
        try {
            const ptr0 = passArray8ToWasm0(key, wasm.__wbindgen_malloc);
            const len0 = WASM_VECTOR_LEN;
            const ret = wasm.olmsession_pickle(this.__wbg_ptr, ptr0, len0);
            var ptr2 = ret[0];
            var len2 = ret[1];
            if (ret[3]) {
                ptr2 = 0; len2 = 0;
                throw takeFromExternrefTable0(ret[2]);
            }
            deferred3_0 = ptr2;
            deferred3_1 = len2;
            return getStringFromWasm0(ptr2, len2);
        } finally {
            wasm.__wbindgen_free(deferred3_0, deferred3_1, 1);
        }
    }
    /**
     * @returns {string}
     */
    get sessionId() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.olmsession_sessionId(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
}
if (Symbol.dispose) OlmSession.prototype[Symbol.dispose] = OlmSession.prototype.free;
function __wbg_get_imports() {
    const import0 = {
        __proto__: null,
        __wbg_Error_67e7344beaa85059: function(arg0, arg1) {
            const ret = Error(getStringFromWasm0(arg0, arg1));
            return ret;
        },
        __wbg___wbindgen_throw_5d9e815e6fdf150f: function(arg0, arg1) {
            throw new Error(getStringFromWasm0(arg0, arg1));
        },
        __wbg_getRandomValues_436a51d0629d84e1: function() { return handleError(function (arg0, arg1) {
            globalThis.crypto.getRandomValues(getArrayU8FromWasm0(arg0, arg1));
        }, arguments); },
        __wbindgen_init_externref_table: function() {
            const table = wasm.__wbindgen_externrefs;
            const offset = table.grow(4);
            table.set(0, undefined);
            table.set(offset + 0, undefined);
            table.set(offset + 1, null);
            table.set(offset + 2, true);
            table.set(offset + 3, false);
        },
    };
    return {
        __proto__: null,
        "./quarel_crypto_bg.js": import0,
    };
}

const DecryptedFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_decrypted_free(ptr, 1));
const InboundResultFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_inboundresult_free(ptr, 1));
const MegolmInboundFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_megolminbound_free(ptr, 1));
const MegolmOutboundFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_megolmoutbound_free(ptr, 1));
const OlmAccountFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_olmaccount_free(ptr, 1));
const OlmSessionFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_olmsession_free(ptr, 1));

function addToExternrefTable0(obj) {
    const idx = wasm.__externref_table_alloc();
    wasm.__wbindgen_externrefs.set(idx, obj);
    return idx;
}

function getArrayU8FromWasm0(ptr, len) {
    ptr = ptr >>> 0;
    return getUint8ArrayMemory0().subarray(ptr / 1, ptr / 1 + len);
}

function getStringFromWasm0(ptr, len) {
    return decodeText(ptr >>> 0, len);
}

let cachedUint8ArrayMemory0 = null;
function getUint8ArrayMemory0() {
    if (cachedUint8ArrayMemory0 === null || cachedUint8ArrayMemory0.byteLength === 0) {
        cachedUint8ArrayMemory0 = new Uint8Array(wasm.memory.buffer);
    }
    return cachedUint8ArrayMemory0;
}

function handleError(f, args) {
    try {
        return f.apply(this, args);
    } catch (e) {
        const idx = addToExternrefTable0(e);
        wasm.__wbindgen_exn_store(idx);
    }
}

function passArray8ToWasm0(arg, malloc) {
    const ptr = malloc(arg.length * 1, 1) >>> 0;
    getUint8ArrayMemory0().set(arg, ptr / 1);
    WASM_VECTOR_LEN = arg.length;
    return ptr;
}

function passStringToWasm0(arg, malloc, realloc) {
    if (realloc === undefined) {
        const buf = cachedTextEncoder.encode(arg);
        const ptr = malloc(buf.length, 1) >>> 0;
        getUint8ArrayMemory0().subarray(ptr, ptr + buf.length).set(buf);
        WASM_VECTOR_LEN = buf.length;
        return ptr;
    }

    let len = arg.length;
    let ptr = malloc(len, 1) >>> 0;

    const mem = getUint8ArrayMemory0();

    let offset = 0;

    for (; offset < len; offset++) {
        const code = arg.charCodeAt(offset);
        if (code > 0x7F) break;
        mem[ptr + offset] = code;
    }
    if (offset !== len) {
        if (offset !== 0) {
            arg = arg.slice(offset);
        }
        ptr = realloc(ptr, len, len = offset + arg.length * 3, 1) >>> 0;
        const view = getUint8ArrayMemory0().subarray(ptr + offset, ptr + len);
        const ret = cachedTextEncoder.encodeInto(arg, view);

        offset += ret.written;
        ptr = realloc(ptr, len, offset, 1) >>> 0;
    }

    WASM_VECTOR_LEN = offset;
    return ptr;
}

function takeFromExternrefTable0(idx) {
    const value = wasm.__wbindgen_externrefs.get(idx);
    wasm.__externref_table_dealloc(idx);
    return value;
}

let cachedTextDecoder = new TextDecoder('utf-8', { ignoreBOM: true, fatal: true });
cachedTextDecoder.decode();
const MAX_SAFARI_DECODE_BYTES = 2146435072;
let numBytesDecoded = 0;
function decodeText(ptr, len) {
    numBytesDecoded += len;
    if (numBytesDecoded >= MAX_SAFARI_DECODE_BYTES) {
        cachedTextDecoder = new TextDecoder('utf-8', { ignoreBOM: true, fatal: true });
        cachedTextDecoder.decode();
        numBytesDecoded = len;
    }
    return cachedTextDecoder.decode(getUint8ArrayMemory0().subarray(ptr, ptr + len));
}

const cachedTextEncoder = new TextEncoder();

if (!('encodeInto' in cachedTextEncoder)) {
    cachedTextEncoder.encodeInto = function (arg, view) {
        const buf = cachedTextEncoder.encode(arg);
        view.set(buf);
        return {
            read: arg.length,
            written: buf.length
        };
    };
}

let WASM_VECTOR_LEN = 0;

let wasmModule, wasmInstance, wasm;
function __wbg_finalize_init(instance, module) {
    wasmInstance = instance;
    wasm = instance.exports;
    wasmModule = module;
    cachedUint8ArrayMemory0 = null;
    wasm.__wbindgen_start();
    return wasm;
}

async function __wbg_load(module, imports) {
    if (typeof Response === 'function' && module instanceof Response) {
        if (!module.ok) {
            throw new Error(`failed to fetch Wasm: ${module.status} ${module.statusText} fetching '${module.url}'`);
        }

        if (typeof WebAssembly.instantiateStreaming === 'function') {
            try {
                return await WebAssembly.instantiateStreaming(module, imports);
            } catch (e) {
                const validResponse = expectedResponseType(module.type);

                if (validResponse && module.headers.get('Content-Type') !== 'application/wasm') {
                    console.warn("`WebAssembly.instantiateStreaming` failed because your server does not serve Wasm with `application/wasm` MIME type. Falling back to `WebAssembly.instantiate` which is slower. Original error:\n", e);

                } else { throw e; }
            }
        }

        const bytes = await module.arrayBuffer();
        return await WebAssembly.instantiate(bytes, imports);
    } else {
        const instance = await WebAssembly.instantiate(module, imports);

        if (instance instanceof WebAssembly.Instance) {
            return { instance, module };
        } else {
            return instance;
        }
    }

    function expectedResponseType(type) {
        switch (type) {
            case 'basic': case 'cors': case 'default': return true;
        }
        return false;
    }
}

function initSync(module) {
    if (wasm !== undefined) return wasm;


    if (module !== undefined) {
        if (Object.getPrototypeOf(module) === Object.prototype) {
            ({module} = module)
        } else {
            console.warn('using deprecated parameters for `initSync()`; pass a single object instead')
        }
    }

    const imports = __wbg_get_imports();
    if (!(module instanceof WebAssembly.Module)) {
        module = new WebAssembly.Module(module);
    }
    const instance = new WebAssembly.Instance(module, imports);
    return __wbg_finalize_init(instance, module);
}

async function __wbg_init(module_or_path) {
    if (wasm !== undefined) return wasm;


    if (module_or_path !== undefined) {
        if (Object.getPrototypeOf(module_or_path) === Object.prototype) {
            ({module_or_path} = module_or_path)
        } else {
            console.warn('using deprecated parameters for the initialization function; pass a single object instead')
        }
    }

    if (module_or_path === undefined) {
        module_or_path = new URL('quarel_crypto_bg.wasm', import.meta.url);
    }
    const imports = __wbg_get_imports();

    if (typeof module_or_path === 'string' || (typeof Request === 'function' && module_or_path instanceof Request) || (typeof URL === 'function' && module_or_path instanceof URL)) {
        module_or_path = fetch(module_or_path);
    }

    const { instance, module } = await __wbg_load(await module_or_path, imports);

    return __wbg_finalize_init(instance, module);
}

export { initSync, __wbg_init as default };
