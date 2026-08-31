CREATE TABLE app_state_metadata (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    initialized_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE instruments (
    id TEXT PRIMARY KEY,
    asset_class TEXT NOT NULL CHECK (asset_class IN ('equity', 'etf', 'crypto')),
    symbol TEXT NOT NULL,
    name TEXT NOT NULL,
    market TEXT NOT NULL,
    exchange TEXT NOT NULL DEFAULT '',
    base_asset TEXT NOT NULL DEFAULT '',
    quote_currency TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'delisted')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (asset_class, market, exchange, symbol)
);

CREATE INDEX idx_instruments_market_symbol ON instruments (market, symbol);
CREATE INDEX idx_instruments_name ON instruments (name);

CREATE TABLE provider_symbols (
    instrument_id TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    provider_symbol TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (instrument_id, provider_id),
    FOREIGN KEY (instrument_id) REFERENCES instruments(id) ON DELETE CASCADE
);

CREATE TABLE watchlist_entries (
    id TEXT PRIMARY KEY,
    instrument_id TEXT NOT NULL UNIQUE,
    quantity TEXT NOT NULL DEFAULT '0',
    cost_price TEXT NOT NULL DEFAULT '0',
    acquired_at TEXT,
    current_price REAL NOT NULL DEFAULT 0,
    previous_close REAL NOT NULL DEFAULT 0,
    open_price REAL NOT NULL DEFAULT 0,
    day_high REAL NOT NULL DEFAULT 0,
    day_low REAL NOT NULL DEFAULT 0,
    change_value REAL NOT NULL DEFAULT 0,
    change_percent REAL NOT NULL DEFAULT 0,
    quote_source TEXT NOT NULL DEFAULT '',
    quote_updated_at TEXT,
    pinned_at TEXT,
    thesis TEXT NOT NULL DEFAULT '',
    tags_json TEXT NOT NULL DEFAULT '[]',
    sort_order INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (instrument_id) REFERENCES instruments(id) ON DELETE RESTRICT
);

CREATE TABLE dca_entries (
    id TEXT PRIMARY KEY,
    watchlist_entry_id TEXT NOT NULL,
    entry_date TEXT NOT NULL,
    amount TEXT NOT NULL,
    shares TEXT NOT NULL,
    price TEXT,
    fee TEXT,
    note TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (watchlist_entry_id) REFERENCES watchlist_entries(id) ON DELETE CASCADE
);

CREATE INDEX idx_dca_entries_watchlist_date ON dca_entries (watchlist_entry_id, entry_date DESC);

CREATE TABLE alerts (
    id TEXT PRIMARY KEY,
    watchlist_entry_id TEXT NOT NULL,
    name TEXT NOT NULL,
    condition TEXT NOT NULL CHECK (condition IN ('above', 'below')),
    threshold TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    triggered INTEGER NOT NULL CHECK (triggered IN (0, 1)),
    last_triggered_at TEXT,
    sort_order INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (watchlist_entry_id) REFERENCES watchlist_entries(id) ON DELETE CASCADE
);

CREATE INDEX idx_alerts_watchlist_entry ON alerts (watchlist_entry_id);

CREATE TABLE settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    hot_cache_ttl_seconds INTEGER NOT NULL,
    cn_quote_source TEXT NOT NULL,
    hk_quote_source TEXT NOT NULL,
    us_quote_source TEXT NOT NULL,
    theme_mode TEXT NOT NULL,
    color_theme TEXT NOT NULL,
    font_preset TEXT NOT NULL,
    amount_display TEXT NOT NULL,
    currency_display TEXT NOT NULL,
    price_color_scheme TEXT NOT NULL,
    locale TEXT NOT NULL,
    proxy_mode TEXT NOT NULL,
    proxy_url TEXT NOT NULL,
    alpha_vantage_api_key TEXT NOT NULL,
    twelve_data_api_key TEXT NOT NULL,
    finnhub_api_key TEXT NOT NULL,
    tiingo_api_key TEXT NOT NULL,
    polygon_api_key TEXT NOT NULL,
    developer_mode INTEGER NOT NULL CHECK (developer_mode IN (0, 1)),
    dashboard_currency TEXT NOT NULL,
    use_native_title_bar INTEGER NOT NULL CHECK (use_native_title_bar IN (0, 1)),
    updated_at TEXT NOT NULL
);

CREATE TABLE pools (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    market TEXT NOT NULL,
    asset_class TEXT NOT NULL CHECK (asset_class IN ('equity', 'etf', 'crypto')),
    pool_type TEXT NOT NULL CHECK (pool_type IN ('builtin', 'index', 'custom')),
    data_version TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE builtin_pool_members (
    pool_id TEXT NOT NULL,
    instrument_id TEXT NOT NULL,
    data_version TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (pool_id, instrument_id),
    FOREIGN KEY (pool_id) REFERENCES pools(id) ON DELETE CASCADE,
    FOREIGN KEY (instrument_id) REFERENCES instruments(id) ON DELETE RESTRICT
);

CREATE TABLE pool_member_overrides (
    pool_id TEXT NOT NULL,
    instrument_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('add', 'exclude')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (pool_id, instrument_id),
    FOREIGN KEY (pool_id) REFERENCES pools(id) ON DELETE CASCADE,
    FOREIGN KEY (instrument_id) REFERENCES instruments(id) ON DELETE RESTRICT
);

CREATE INDEX idx_pool_member_overrides_action ON pool_member_overrides (pool_id, action);
