<script setup lang="ts">
    import { computed, ref, watch } from 'vue';
    import Button from 'primevue/button';
    import Dialog from 'primevue/dialog';
    import InputText from 'primevue/inputtext';
    import Select from 'primevue/select';
    import Tag from 'primevue/tag';

    import { api } from '../../api';
    import { useI18n } from '../../i18n';
    import type {
        AddPoolMemberRequest,
        HotCategory,
        HotItem,
        HotListResponse,
        HotMarketGroup,
        InstrumentPool,
        PageResponse,
        PoolMember,
        PoolMemberStatus,
        UpdatePoolMemberRequest,
    } from '../../types';

    const props = defineProps<{
        visible: boolean;
        marketGroup: HotMarketGroup;
    }>();

    const emit = defineEmits<{
        (event: 'update:visible', value: boolean): void;
        (event: 'changed'): void;
    }>();

    const { t } = useI18n();
    const pools = ref<InstrumentPool[]>([]);
    const selectedPoolId = ref('');
    const memberStatus = ref<PoolMemberStatus>('active');
    const members = ref<PoolMember[]>([]);
    const memberPage = ref(1);
    const memberTotal = ref(0);
    const memberHasMore = ref(false);
    const loadingPools = ref(false);
    const loadingMembers = ref(false);
    const mutatingId = ref('');
    const pendingDeleteId = ref('');
    const error = ref('');
    const searchQuery = ref('');
    const searching = ref(false);
    const searchResults = ref<HotItem[]>([]);
    const searchWarning = ref('');
    const editingId = ref('');
    const editSymbol = ref('');
    const editName = ref('');

    const visibleProxy = computed({
        get: () => props.visible,
        set: (value: boolean) => emit('update:visible', value),
    });

    const builtinPoolOrder: Record<string, number> = {
        'pool-cn-a-ranking': 0,
        'pool-cn-etf-ranking': 1,
        'pool-hk-ranking': 0,
        'pool-hk-etf': 1,
        'pool-us-sp500': 0,
        'pool-us-nasdaq-100': 1,
        'pool-us-dow-30': 2,
        'pool-us-etf': 3,
    };

    const manageablePools = computed(() =>
        pools.value
            .filter((pool) => {
                if (pool.type === 'custom') return false;
                if (props.marketGroup === 'cn') return pool.market.startsWith('CN-');
                if (props.marketGroup === 'hk') return pool.market.startsWith('HK-');
                return pool.market.startsWith('US-');
            })
            .slice()
            .sort((left, right) => (builtinPoolOrder[left.id] ?? 99) - (builtinPoolOrder[right.id] ?? 99)),
    );

    const poolOptions = computed(() =>
        manageablePools.value.map((pool) => ({
            label: poolDisplayName(pool),
            value: pool.id,
        })),
    );

    const selectedPool = computed(() => pools.value.find((pool) => pool.id === selectedPoolId.value));
    const canSearch = computed(
        () => searchQuery.value.trim().length > 0 && Boolean(selectedPool.value) && !searching.value,
    );

    watch(
        () => props.visible,
        (visible) => {
            if (visible) void loadPools();
        },
    );

    watch(
        () => props.marketGroup,
        () => {
            if (props.visible) selectDefaultPool();
        },
    );

    watch([selectedPoolId, memberStatus], () => {
        pendingDeleteId.value = '';
        editingId.value = '';
        searchResults.value = [];
        searchWarning.value = '';
        if (selectedPoolId.value) void loadMembers(false);
    });

    async function loadPools(): Promise<void> {
        loadingPools.value = true;
        error.value = '';
        try {
            const response = await api<PageResponse<InstrumentPool>>('/api/pools?page=1&pageSize=100');
            pools.value = response.items;
            selectDefaultPool();
        } catch (requestError) {
            error.value = requestError instanceof Error ? requestError.message : t('poolManager.loadFailed');
        } finally {
            loadingPools.value = false;
        }
    }

    function selectDefaultPool(): void {
        if (manageablePools.value.some((pool) => pool.id === selectedPoolId.value)) {
            void loadMembers(false);
            return;
        }
        const preferred = manageablePools.value[0];
        selectedPoolId.value = preferred?.id ?? '';
    }

    const membersRequestId = ref(0);

    async function loadMembers(append: boolean): Promise<void> {
        if (!selectedPoolId.value) return;
        if (!append) {
            membersRequestId.value += 1;
        }
        const requestId = membersRequestId.value;
        const poolId = selectedPoolId.value;
        const status = memberStatus.value;
        loadingMembers.value = true;
        error.value = '';
        const nextPage = append ? memberPage.value + 1 : 1;
        try {
            const response = await api<PageResponse<PoolMember>>(
                `/api/pools/${encodeURIComponent(poolId)}/members?status=${status}&page=${nextPage}&pageSize=100`,
            );
            if (requestId !== membersRequestId.value) return;
            members.value = append ? [...members.value, ...response.items] : response.items;
            memberPage.value = response.page;
            memberTotal.value = response.total;
            memberHasMore.value = response.hasMore;
        } catch (requestError) {
            if (requestId !== membersRequestId.value) return;
            error.value = requestError instanceof Error ? requestError.message : t('poolManager.loadFailed');
        } finally {
            if (requestId === membersRequestId.value) {
                loadingMembers.value = false;
            }
        }
    }

    async function searchCandidates(): Promise<void> {
        if (!canSearch.value || !selectedPool.value) return;
        searching.value = true;
        searchResults.value = [];
        searchWarning.value = '';
        error.value = '';
        try {
            const category = discoveryCategory(selectedPool.value.id);
            const params = new URLSearchParams({
                category,
                q: searchQuery.value.trim(),
                page: '1',
                pageSize: '10',
                force: '1',
            });
            const response = await api<HotListResponse>(`/api/hot?${params.toString()}`, { timeoutMs: 12000 });
            searchResults.value = response.items ?? [];
            if (!searchResults.value.length) searchWarning.value = t('poolManager.noSearchResults');
        } catch (requestError) {
            searchWarning.value =
                requestError instanceof Error ? requestError.message : t('poolManager.searchTemporarilyUnavailable');
        } finally {
            searching.value = false;
        }
    }

    async function addCandidate(item?: HotItem): Promise<void> {
        if (!selectedPool.value) return;
        const symbol = item?.symbol ?? searchQuery.value.trim();
        if (!symbol) return;
        const payload: AddPoolMemberRequest = {
            assetClass: selectedPool.value.assetClass,
            symbol,
            name: item?.name || symbol,
            market: selectedPool.value.market,
            quoteCurrency: item?.currency || defaultCurrency(selectedPool.value.market),
        };
        mutatingId.value = `add:${symbol}`;
        error.value = '';
        try {
            await api<PoolMember>(`/api/pools/${encodeURIComponent(selectedPool.value.id)}/members`, {
                method: 'POST',
                body: JSON.stringify(payload),
            });
            searchQuery.value = '';
            searchResults.value = [];
            searchWarning.value = '';
            memberStatus.value = 'active';
            await loadMembers(false);
            emit('changed');
        } catch (requestError) {
            error.value = requestError instanceof Error ? requestError.message : t('poolManager.saveFailed');
        } finally {
            mutatingId.value = '';
        }
    }

    async function removeMember(member: PoolMember): Promise<void> {
        if (pendingDeleteId.value !== member.instrument.id) {
            pendingDeleteId.value = member.instrument.id;
            return;
        }
        mutatingId.value = member.instrument.id;
        error.value = '';
        try {
            await api(
                `/api/pools/${encodeURIComponent(member.poolId)}/members/${encodeURIComponent(member.instrument.id)}`,
                {
                    method: 'DELETE',
                },
            );
            pendingDeleteId.value = '';
            await loadMembers(false);
            emit('changed');
        } catch (requestError) {
            error.value = requestError instanceof Error ? requestError.message : t('poolManager.deleteFailed');
        } finally {
            mutatingId.value = '';
        }
    }

    async function restoreMember(member: PoolMember): Promise<void> {
        mutatingId.value = member.instrument.id;
        error.value = '';
        try {
            await api<PoolMember>(
                `/api/pools/${encodeURIComponent(member.poolId)}/members/${encodeURIComponent(member.instrument.id)}/restore`,
                { method: 'POST' },
            );
            await loadMembers(false);
            emit('changed');
        } catch (requestError) {
            error.value = requestError instanceof Error ? requestError.message : t('poolManager.restoreFailed');
        } finally {
            mutatingId.value = '';
        }
    }

    function beginEdit(member: PoolMember): void {
        editingId.value = member.instrument.id;
        editSymbol.value = member.instrument.symbol;
        editName.value = member.instrument.name;
        error.value = '';
    }

    function cancelEdit(): void {
        editingId.value = '';
        editSymbol.value = '';
        editName.value = '';
    }

    async function saveEdit(member: PoolMember, resetName = false): Promise<void> {
        if (editingId.value !== member.instrument.id && !resetName) return;
        const payload: UpdatePoolMemberRequest = {};
        if (!resetName && editSymbol.value.trim() && editSymbol.value.trim() !== member.instrument.symbol) {
            payload.symbol = editSymbol.value.trim();
        }
        if (resetName) {
            payload.resetName = true;
        } else if (editName.value.trim() && editName.value.trim() !== member.instrument.name) {
            payload.name = editName.value.trim();
        }
        if (!payload.symbol && !payload.name && !payload.resetName) {
            cancelEdit();
            return;
        }
        mutatingId.value = member.instrument.id;
        error.value = '';
        try {
            await api<PoolMember>(
                `/api/pools/${encodeURIComponent(member.poolId)}/members/${encodeURIComponent(member.instrument.id)}`,
                {
                    method: 'PUT',
                    body: JSON.stringify(payload),
                },
            );
            cancelEdit();
            await loadMembers(false);
            emit('changed');
        } catch (requestError) {
            error.value = requestError instanceof Error ? requestError.message : t('poolManager.editFailed');
        } finally {
            mutatingId.value = '';
        }
    }

    function discoveryCategory(poolId: string): HotCategory {
        const categories: Record<string, HotCategory> = {
            'pool-cn-a-ranking': 'cn-a',
            'pool-cn-etf-ranking': 'cn-etf',
            'pool-hk-ranking': 'hk',
            'pool-hk-etf': 'hk-etf',
            'pool-us-sp500': 'us-sp500',
            'pool-us-nasdaq-100': 'us-nasdaq',
            'pool-us-dow-30': 'us-dow',
            'pool-us-etf': 'us-etf',
        };
        return categories[poolId] ?? 'cn-a';
    }

    function poolDisplayName(pool: InstrumentPool): string {
        const category = discoveryCategory(pool.id);
        const translated = t(`options.hotCategory.${category}`);
        return translated === `options.hotCategory.${category}` ? pool.name : translated;
    }

    function defaultCurrency(market: string): string {
        if (market.startsWith('HK-')) return 'HKD';
        if (market.startsWith('US-')) return 'USD';
        return 'CNY';
    }

    function poolTypeLabel(poolType: InstrumentPool['type']): string {
        return t(`poolManager.poolType.${poolType}`);
    }

    function sourceLabel(source: PoolMember['source']): string {
        return t(`poolManager.source.${source}`);
    }
</script>

<template>
    <Dialog
        v-model:visible="visibleProxy"
        modal
        :closable="false"
        :header="t('poolManager.title')"
        :style="{ width: 'min(860px, 94vw)' }"
        :breakpoints="{ '760px': '96vw' }"
        class="desk-dialog"
    >
        <div class="pool-manager-layout">
            <aside class="pool-manager-sidebar">
                <label for="pool-manager-select">{{ t('poolManager.poolLabel') }}</label>
                <Select
                    id="pool-manager-select"
                    v-model="selectedPoolId"
                    :options="poolOptions"
                    option-label="label"
                    option-value="value"
                    :loading="loadingPools"
                    class="pool-select"
                />
                <div v-if="selectedPool" class="pool-meta">
                    <strong>{{ poolDisplayName(selectedPool) }}</strong>
                    <span>{{ selectedPool.market }}</span>
                    <Tag :value="poolTypeLabel(selectedPool.type)" severity="secondary" />
                </div>
                <p class="pool-manager-note">{{ t('poolManager.persistenceNote') }}</p>
                <p class="pool-manager-note">{{ t('poolManager.editBuiltinNote') }}</p>
            </aside>

            <section class="pool-manager-content">
                <div class="pool-member-tabs" role="tablist" :aria-label="t('poolManager.memberTabs')">
                    <button
                        type="button"
                        role="tab"
                        :aria-selected="memberStatus === 'active'"
                        :class="{ active: memberStatus === 'active' }"
                        @click="memberStatus = 'active'"
                    >
                        {{ t('poolManager.activeTab') }}
                    </button>
                    <button
                        type="button"
                        role="tab"
                        :aria-selected="memberStatus === 'excluded'"
                        :class="{ active: memberStatus === 'excluded' }"
                        @click="memberStatus = 'excluded'"
                    >
                        {{ t('poolManager.excludedTab') }}
                    </button>
                </div>

                <div v-if="memberStatus === 'active'" class="pool-add-section">
                    <label for="pool-symbol-search">{{ t('poolManager.addLabel') }}</label>
                    <div class="pool-search-row">
                        <InputText
                            id="pool-symbol-search"
                            v-model="searchQuery"
                            :placeholder="t('poolManager.searchPlaceholder')"
                            @keydown.enter="searchCandidates"
                        />
                        <Button
                            size="small"
                            icon="pi pi-search"
                            :label="t('poolManager.search')"
                            :loading="searching"
                            :disabled="!canSearch"
                            @click="searchCandidates"
                        />
                    </div>
                    <p v-if="searchWarning" class="pool-inline-warning">{{ searchWarning }}</p>
                    <div v-if="searchResults.length" class="pool-search-results">
                        <div
                            v-for="item in searchResults"
                            :key="`${item.market}:${item.symbol}`"
                            class="pool-search-result"
                        >
                            <div>
                                <strong>{{ item.name }}</strong>
                                <span>{{ item.market }} / {{ item.symbol }}</span>
                            </div>
                            <Button
                                size="small"
                                text
                                icon="pi pi-plus"
                                :label="t('common.add')"
                                :loading="mutatingId === `add:${item.symbol}`"
                                @click="addCandidate(item)"
                            />
                        </div>
                    </div>
                    <Button
                        v-else-if="searchWarning && searchQuery.trim()"
                        size="small"
                        text
                        icon="pi pi-save"
                        :label="t('poolManager.saveAnyway')"
                        :loading="mutatingId === `add:${searchQuery.trim()}`"
                        @click="addCandidate()"
                    />
                </div>

                <p v-if="error" class="pool-manager-error">{{ error }}</p>
                <div class="pool-member-summary">
                    <span>{{ t('poolManager.memberCount', { count: members.length, total: memberTotal }) }}</span>
                    <Button
                        size="small"
                        text
                        icon="pi pi-refresh"
                        :aria-label="t('common.refresh')"
                        :title="t('common.refresh')"
                        :loading="loadingMembers"
                        @click="loadMembers(false)"
                    />
                </div>

                <div v-if="members.length" class="pool-member-list">
                    <div
                        v-for="member in members"
                        :key="member.instrument.id"
                        class="pool-member-row"
                        :class="{ 'pool-member-editing': editingId === member.instrument.id }"
                    >
                        <div v-if="editingId === member.instrument.id" class="pool-member-edit-form">
                            <label :for="`pool-edit-symbol-${member.instrument.id}`">
                                {{ t('poolManager.editSymbolLabel') }}
                            </label>
                            <InputText
                                :id="`pool-edit-symbol-${member.instrument.id}`"
                                v-model="editSymbol"
                                size="small"
                                class="pool-edit-input"
                            />
                            <label :for="`pool-edit-name-${member.instrument.id}`">
                                {{ t('poolManager.editNameLabel') }}
                            </label>
                            <InputText
                                :id="`pool-edit-name-${member.instrument.id}`"
                                v-model="editName"
                                size="small"
                                class="pool-edit-input"
                            />
                        </div>
                        <div v-else class="pool-member-identity">
                            <strong>{{ member.instrument.name }}</strong>
                            <span>{{ member.instrument.market }} / {{ member.instrument.symbol }}</span>
                        </div>
                        <Tag
                            v-if="editingId !== member.instrument.id"
                            :value="sourceLabel(member.source)"
                            severity="secondary"
                        />
                        <div class="pool-member-actions">
                            <template v-if="editingId === member.instrument.id">
                                <Button
                                    size="small"
                                    :label="t('poolManager.saveEdit')"
                                    :loading="mutatingId === member.instrument.id"
                                    @click="saveEdit(member)"
                                />
                                <Button size="small" text :label="t('common.cancel')" @click="cancelEdit" />
                            </template>
                            <template v-else-if="memberStatus === 'active'">
                                <Button
                                    v-if="member.instrument.hasCustomName"
                                    size="small"
                                    text
                                    :label="t('poolManager.resetName')"
                                    :disabled="Boolean(mutatingId)"
                                    @click="saveEdit(member, true)"
                                />
                                <Button
                                    size="small"
                                    text
                                    icon="pi pi-pencil"
                                    :label="t('poolManager.edit')"
                                    :disabled="Boolean(mutatingId)"
                                    @click="beginEdit(member)"
                                />
                                <Button
                                    v-if="pendingDeleteId !== member.instrument.id"
                                    size="small"
                                    text
                                    severity="danger"
                                    icon="pi pi-trash"
                                    :label="t('common.delete')"
                                    :disabled="Boolean(mutatingId)"
                                    @click="removeMember(member)"
                                />
                                <template v-else>
                                    <Button
                                        size="small"
                                        severity="danger"
                                        :label="t('poolManager.confirmDelete')"
                                        :loading="mutatingId === member.instrument.id"
                                        @click="removeMember(member)"
                                    />
                                    <Button
                                        size="small"
                                        text
                                        :label="t('common.cancel')"
                                        @click="pendingDeleteId = ''"
                                    />
                                </template>
                            </template>
                            <Button
                                v-else
                                size="small"
                                text
                                icon="pi pi-replay"
                                :label="t('poolManager.restore')"
                                :loading="mutatingId === member.instrument.id"
                                @click="restoreMember(member)"
                            />
                        </div>
                    </div>
                </div>
                <div v-else-if="!loadingMembers" class="pool-empty-state">
                    <i :class="memberStatus === 'active' ? 'pi pi-list' : 'pi pi-eye-slash'"></i>
                    <strong>{{
                        memberStatus === 'active' ? t('poolManager.emptyActive') : t('poolManager.emptyExcluded')
                    }}</strong>
                    <span>{{
                        memberStatus === 'active'
                            ? t('poolManager.emptyActiveHint')
                            : t('poolManager.emptyExcludedHint')
                    }}</span>
                </div>

                <Button
                    v-if="memberHasMore"
                    size="small"
                    text
                    :label="t('poolManager.loadMore')"
                    :loading="loadingMembers"
                    @click="loadMembers(true)"
                />
            </section>
        </div>
        <template #footer>
            <Button size="small" text :label="t('common.close')" @click="visibleProxy = false" />
        </template>
    </Dialog>
</template>

<style scoped>
    .pool-manager-layout {
        display: grid;
        grid-template-columns: minmax(190px, 0.75fr) minmax(0, 2fr);
        min-height: 520px;
    }

    .pool-manager-sidebar {
        padding: 18px 18px 18px 2px;
        border-right: 1px solid var(--border);
    }

    .pool-manager-sidebar label,
    .pool-add-section > label {
        display: block;
        margin-bottom: 8px;
        color: var(--muted);
        font-size: 12px;
        font-weight: 700;
    }

    .pool-select {
        width: 100%;
    }

    .pool-meta {
        display: flex;
        align-items: flex-start;
        flex-direction: column;
        gap: 6px;
        margin-top: 18px;
    }

    .pool-meta span,
    .pool-manager-note,
    .pool-member-identity span,
    .pool-search-result span,
    .pool-empty-state span {
        color: var(--muted);
        font-size: 12px;
    }

    .pool-manager-note {
        margin: 24px 0 0;
        line-height: 1.6;
    }

    .pool-manager-content {
        min-width: 0;
        padding: 18px 0 18px 22px;
    }

    .pool-member-tabs {
        display: flex;
        gap: 4px;
        width: fit-content;
        padding: 3px;
        border: 1px solid var(--border);
        border-radius: var(--radius-panel);
        background: var(--panel-soft);
    }

    .pool-member-tabs button {
        padding: 6px 12px;
        border: 0;
        border-radius: var(--radius-micro);
        background: transparent;
        color: var(--muted);
        cursor: pointer;
        font: inherit;
        font-size: 12px;
        font-weight: 700;
    }

    .pool-member-tabs button.active {
        background: var(--panel-strong);
        color: var(--ink);
        box-shadow: 0 1px 3px color-mix(in srgb, var(--ink) 12%, transparent);
    }

    .pool-add-section {
        margin-top: 18px;
        padding-bottom: 18px;
        border-bottom: 1px solid var(--border);
    }

    .pool-search-row {
        display: grid;
        grid-template-columns: minmax(0, 1fr) auto;
        gap: 8px;
    }

    .pool-search-results {
        max-height: 176px;
        margin-top: 10px;
        overflow: auto;
        border-left: 2px solid var(--accent);
    }

    .pool-search-result,
    .pool-member-row {
        display: grid;
        align-items: center;
        gap: 12px;
        padding: 10px 12px;
        border-bottom: 1px solid var(--border);
    }

    .pool-search-result {
        grid-template-columns: minmax(0, 1fr) auto;
    }

    .pool-search-result > div,
    .pool-member-identity {
        display: flex;
        min-width: 0;
        flex-direction: column;
        gap: 3px;
    }

    .pool-inline-warning,
    .pool-manager-error {
        margin: 10px 0 0;
        color: var(--rise);
        font-size: 12px;
        line-height: 1.5;
    }

    .pool-member-summary {
        display: flex;
        align-items: center;
        justify-content: space-between;
        min-height: 42px;
        color: var(--muted);
        font-size: 12px;
    }

    .pool-member-list {
        max-height: 330px;
        overflow: auto;
    }

    .pool-member-row {
        grid-template-columns: minmax(0, 1fr) auto minmax(112px, auto);
        padding-inline: 0;
    }

    .pool-member-row.pool-member-editing {
        grid-template-columns: minmax(0, 1fr) minmax(160px, auto);
    }

    .pool-member-edit-form {
        display: grid;
        grid-template-columns: auto minmax(0, 1fr);
        gap: 6px 10px;
        align-items: center;
    }

    .pool-member-edit-form label {
        color: var(--muted);
        font-size: 11px;
        font-weight: 700;
    }

    .pool-edit-input {
        width: 100%;
    }

    .pool-member-actions {
        display: flex;
        justify-content: flex-end;
        gap: 4px;
    }

    .pool-empty-state {
        display: flex;
        min-height: 190px;
        align-items: center;
        justify-content: center;
        flex-direction: column;
        gap: 8px;
        text-align: center;
    }

    .pool-empty-state i {
        color: var(--muted);
        font-size: 24px;
    }

    @media (max-width: 760px) {
        .pool-manager-layout {
            grid-template-columns: 1fr;
        }

        .pool-manager-sidebar {
            padding: 12px 0 16px;
            border-right: 0;
            border-bottom: 1px solid var(--border);
        }

        .pool-manager-content {
            padding: 16px 0 0;
        }

        .pool-member-row {
            grid-template-columns: minmax(0, 1fr) auto;
        }

        .pool-member-actions {
            grid-column: 1 / -1;
            justify-content: flex-start;
        }
    }
</style>
