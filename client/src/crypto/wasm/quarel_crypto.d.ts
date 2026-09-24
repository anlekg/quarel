/* tslint:disable */
/* eslint-disable */

export class Decrypted {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    readonly index: number;
    readonly plaintext: string;
}

export class InboundResult {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    /**
     * The new session (can be taken once).
     */
    takeSession(): OlmSession | undefined;
    readonly plaintext: string;
}

export class MegolmInbound {
    free(): void;
    [Symbol.dispose](): void;
    decrypt(ciphertext: string): Decrypted;
    exportAt(index: number): string | undefined;
    static fromPickle(pickle: string, key: Uint8Array): MegolmInbound;
    /**
     * From an exported key (history transfer, backup).
     */
    static import(exported: string): MegolmInbound;
    /**
     * From a shared session key (room_key).
     */
    constructor(session_key: string);
    pickle(key: Uint8Array): string;
    readonly firstKnownIndex: number;
    readonly sessionId: string;
}

export class MegolmOutbound {
    free(): void;
    [Symbol.dispose](): void;
    encrypt(plaintext: string): string;
    static fromPickle(pickle: string, key: Uint8Array): MegolmOutbound;
    constructor();
    pickle(key: Uint8Array): string;
    readonly sessionId: string;
    /**
     * The key to share (at the current index), base64.
     */
    readonly sessionKey: string;
}

export class OlmAccount {
    free(): void;
    [Symbol.dispose](): void;
    static fromPickle(pickle: string, key: Uint8Array): OlmAccount;
    generateOneTimeKeys(count: number): void;
    /**
     * Opens a session from a pre-key message (type 0) and decrypts it; the
     * one-time key it used is removed.
     */
    inboundSession(identity_key: string, body: string): InboundResult;
    markKeysAsPublished(): void;
    constructor();
    /**
     * Unpublished one-time keys, as JSON {key id: key}.
     */
    oneTimeKeys(): string;
    outboundSession(identity_key: string, one_time_key: string): OlmSession;
    pickle(key: Uint8Array): string;
    /**
     * Ed25519 signature (base64) of message by the device key.
     */
    sign(message: Uint8Array): string;
    readonly curve25519: string;
    readonly ed25519: string;
}

export class OlmSession {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    decrypt(message_type: number, body: string): string;
    /**
     * Encrypts; returns JSON {"type": 0|1, "body": "…"}.
     */
    encrypt(plaintext: string): string;
    static fromPickle(pickle: string, key: Uint8Array): OlmSession;
    pickle(key: Uint8Array): string;
    readonly sessionId: string;
}

export type InitInput = RequestInfo | URL | Response | BufferSource | WebAssembly.Module;

export interface InitOutput {
    readonly memory: WebAssembly.Memory;
    readonly __wbg_decrypted_free: (a: number, b: number) => void;
    readonly __wbg_inboundresult_free: (a: number, b: number) => void;
    readonly __wbg_megolminbound_free: (a: number, b: number) => void;
    readonly __wbg_megolmoutbound_free: (a: number, b: number) => void;
    readonly __wbg_olmaccount_free: (a: number, b: number) => void;
    readonly __wbg_olmsession_free: (a: number, b: number) => void;
    readonly decrypted_index: (a: number) => number;
    readonly decrypted_plaintext: (a: number) => [number, number];
    readonly inboundresult_plaintext: (a: number) => [number, number];
    readonly inboundresult_takeSession: (a: number) => number;
    readonly megolminbound_decrypt: (a: number, b: number, c: number) => [number, number, number];
    readonly megolminbound_exportAt: (a: number, b: number) => [number, number];
    readonly megolminbound_firstKnownIndex: (a: number) => number;
    readonly megolminbound_fromPickle: (a: number, b: number, c: number, d: number) => [number, number, number];
    readonly megolminbound_import: (a: number, b: number) => [number, number, number];
    readonly megolminbound_new: (a: number, b: number) => [number, number, number];
    readonly megolminbound_pickle: (a: number, b: number, c: number) => [number, number, number, number];
    readonly megolminbound_sessionId: (a: number) => [number, number];
    readonly megolmoutbound_encrypt: (a: number, b: number, c: number) => [number, number];
    readonly megolmoutbound_fromPickle: (a: number, b: number, c: number, d: number) => [number, number, number];
    readonly megolmoutbound_new: () => number;
    readonly megolmoutbound_pickle: (a: number, b: number, c: number) => [number, number, number, number];
    readonly megolmoutbound_sessionId: (a: number) => [number, number];
    readonly megolmoutbound_sessionKey: (a: number) => [number, number];
    readonly olmaccount_curve25519: (a: number) => [number, number];
    readonly olmaccount_ed25519: (a: number) => [number, number];
    readonly olmaccount_fromPickle: (a: number, b: number, c: number, d: number) => [number, number, number];
    readonly olmaccount_generateOneTimeKeys: (a: number, b: number) => void;
    readonly olmaccount_inboundSession: (a: number, b: number, c: number, d: number, e: number) => [number, number, number];
    readonly olmaccount_markKeysAsPublished: (a: number) => void;
    readonly olmaccount_new: () => number;
    readonly olmaccount_oneTimeKeys: (a: number) => [number, number];
    readonly olmaccount_outboundSession: (a: number, b: number, c: number, d: number, e: number) => [number, number, number];
    readonly olmaccount_pickle: (a: number, b: number, c: number) => [number, number, number, number];
    readonly olmaccount_sign: (a: number, b: number, c: number) => [number, number];
    readonly olmsession_decrypt: (a: number, b: number, c: number, d: number) => [number, number, number, number];
    readonly olmsession_encrypt: (a: number, b: number, c: number) => [number, number, number, number];
    readonly olmsession_fromPickle: (a: number, b: number, c: number, d: number) => [number, number, number];
    readonly olmsession_pickle: (a: number, b: number, c: number) => [number, number, number, number];
    readonly olmsession_sessionId: (a: number) => [number, number];
    readonly __wbindgen_exn_store: (a: number) => void;
    readonly __externref_table_alloc: () => number;
    readonly __wbindgen_externrefs: WebAssembly.Table;
    readonly __wbindgen_free: (a: number, b: number, c: number) => void;
    readonly __wbindgen_malloc: (a: number, b: number) => number;
    readonly __wbindgen_realloc: (a: number, b: number, c: number, d: number) => number;
    readonly __externref_table_dealloc: (a: number) => void;
    readonly __wbindgen_start: () => void;
}

export type SyncInitInput = BufferSource | WebAssembly.Module;

/**
 * Instantiates the given `module`, which can either be bytes or
 * a precompiled `WebAssembly.Module`.
 *
 * @param {{ module: SyncInitInput }} module - Passing `SyncInitInput` directly is deprecated.
 *
 * @returns {InitOutput}
 */
export function initSync(module: { module: SyncInitInput } | SyncInitInput): InitOutput;

/**
 * If `module_or_path` is {RequestInfo} or {URL}, makes a request and
 * for everything else, calls `WebAssembly.instantiate` directly.
 *
 * @param {{ module_or_path: InitInput | Promise<InitInput> }} module_or_path - Passing `InitInput` directly is deprecated.
 *
 * @returns {Promise<InitOutput>}
 */
export default function __wbg_init (module_or_path?: { module_or_path: InitInput | Promise<InitInput> } | InitInput | Promise<InitInput>): Promise<InitOutput>;
