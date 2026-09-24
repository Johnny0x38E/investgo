<script setup lang="ts">
    import { computed, reactive, ref, watch } from 'vue';
    import Button from 'primevue/button';
    import Dialog from 'primevue/dialog';
    import InputNumber from 'primevue/inputnumber';
    import InputText from 'primevue/inputtext';

    import { formatMoney, formatNumber, formatPercent, formatUnitPrice, resolvedLocale } from '../../format';
    import { useI18n } from '../../i18n';
    import type { DCAEntryRow, WatchlistItem } from '../../types';

    type EntryMode = 'price' | 'shares';

    interface SortedEntry extends DCAEntryRow {
        sourceIndex: number;
        effectivePrice?: number;
    }

    const props = defineProps<{
        visible: boolean;
        item: WatchlistItem | null;
        saving: boolean;
    }>();

    const emit = defineEmits<{
        (event: 'update:visible', value: boolean): void;
        (event: 'save', entries: DCAEntryRow[]): void;
    }>();

    const visibleProxy = computed({
        get: () => props.visible,
        set: (value: boolean) => emit('update:visible', value),
    });

    const { t } = useI18n();
    const entryMode = ref<EntryMode>('price');
    const entryExpanded = ref(false);
    const editingId = ref('');
    const showOptional = ref(false);
    const pendingDeleteId = ref('');
    const draft = reactive<DCAEntryRow>(emptyDraft());

    const dialogHeader = computed(() => {
        if (!props.item) return t('dialogs.dcaDetail.title');
        return t('dialogs.dcaDetail.titleWithName', { name: props.item.name || props.item.symbol });
    });

    const entryRows = computed<DCAEntryRow[]>(() =>
        (props.item?.dcaEntries ?? [])
            .filter((entry) => entry.amount > 0 && entry.shares > 0)
            .map((entry) => ({
                id: entry.id,
                date: toInputDate(entry.date),
                amount: entry.amount,
                shares: entry.shares,
                price: entry.price && entry.price > 0 ? entry.price : null,
                fee: entry.fee && entry.fee > 0 ? entry.fee : null,
                note: entry.note ?? '',
            })),
    );

    const sortedEntries = computed<SortedEntry[]>(() =>
        entryRows.value
            .map((entry, sourceIndex) => ({
                ...entry,
                sourceIndex,
                effectivePrice:
                    (entry.price ?? 0) > 0
                        ? (entry.price ?? 0)
                        : ((entry.amount ?? 0) - (entry.fee ?? 0)) / (entry.shares ?? 1),
            }))
            .sort((left, right) => right.date.localeCompare(left.date) || right.sourceIndex - left.sourceIndex),
    );

    const summary = computed(() => props.item?.dcaSummary ?? null);
    const netAmount = computed(() => Math.max((draft.amount ?? 0) - (draft.fee ?? 0), 0));
    const derivedShares = computed(() => {
        const price = draft.price ?? 0;
        return price > 0 ? netAmount.value / price : 0;
    });
    const derivedPrice = computed(() => {
        const shares = draft.shares ?? 0;
        return shares > 0 ? netAmount.value / shares : 0;
    });
    const canSubmit = computed(() => {
        if (!draft.date || (draft.amount ?? 0) <= 0 || netAmount.value <= 0) return false;
        return entryMode.value === 'price' ? (draft.price ?? 0) > 0 : (draft.shares ?? 0) > 0;
    });

    watch(
        () => [props.visible, props.item?.id, props.item?.updatedAt] as const,
        ([visible]) => {
            if (!visible) return;
            resetDraft();
            entryExpanded.value = entryRows.value.length === 0;
        },
    );

    function todayInputDate(): string {
        const now = new Date();
        const year = String(now.getFullYear());
        const month = String(now.getMonth() + 1).padStart(2, '0');
        const day = String(now.getDate()).padStart(2, '0');
        return `${year}-${month}-${day}`;
    }

    function emptyDraft(): DCAEntryRow {
        return { id: '', date: todayInputDate(), amount: null, shares: null, price: null, fee: null, note: '' };
    }

    function toInputDate(value: string): string {
        if (/^\d{4}-\d{2}-\d{2}$/.test(value)) return value;
        const parsed = new Date(value);
        if (Number.isNaN(parsed.getTime())) return value.substring(0, 10);
        const year = String(parsed.getFullYear());
        const month = String(parsed.getMonth() + 1).padStart(2, '0');
        const day = String(parsed.getDate()).padStart(2, '0');
        return `${year}-${month}-${day}`;
    }

    function resetDraft(): void {
        Object.assign(draft, emptyDraft());
        entryMode.value = 'price';
        editingId.value = '';
        showOptional.value = false;
        pendingDeleteId.value = '';
    }

    function collapseEntry(): void {
        resetDraft();
        entryExpanded.value = false;
    }

    function copyLastEntry(): void {
        const latest = sortedEntries.value[0];
        if (!latest) return;
        Object.assign(draft, {
            id: '',
            date: todayInputDate(),
            amount: latest.amount,
            shares: latest.shares,
            price: latest.price,
            fee: latest.fee,
            note: latest.note,
        });
        entryMode.value = (latest.price ?? 0) > 0 ? 'price' : 'shares';
        editingId.value = '';
        showOptional.value = (latest.fee ?? 0) > 0 || Boolean(latest.note);
    }

    function startEdit(entry: SortedEntry): void {
        Object.assign(draft, {
            id: entry.id,
            date: entry.date,
            amount: entry.amount,
            shares: entry.shares,
            price: entry.price,
            fee: entry.fee,
            note: entry.note,
        });
        entryMode.value = (entry.price ?? 0) > 0 ? 'price' : 'shares';
        editingId.value = entry.id;
        showOptional.value = (entry.fee ?? 0) > 0 || Boolean(entry.note);
        pendingDeleteId.value = '';
        entryExpanded.value = true;
    }

    function submitDraft(): void {
        if (!canSubmit.value || props.saving) return;

        const nextEntry: DCAEntryRow = {
            id: editingId.value || `tmp-${Date.now()}`,
            date: draft.date,
            amount: draft.amount,
            shares: entryMode.value === 'price' ? derivedShares.value : draft.shares,
            price: entryMode.value === 'price' ? draft.price : null,
            fee: (draft.fee ?? 0) > 0 ? draft.fee : null,
            note: draft.note.trim(),
        };
        const nextEntries = editingId.value
            ? entryRows.value.map((entry) => (entry.id === editingId.value ? nextEntry : entry))
            : [...entryRows.value, nextEntry];
        emit('save', nextEntries);
    }

    function confirmDelete(entryId: string): void {
        if (props.saving) return;
        emit(
            'save',
            entryRows.value.filter((entry) => entry.id !== entryId),
        );
    }

    function buyPrice(entry: SortedEntry): string {
        if (!props.item) return '—';
        const price = (entry.price ?? 0) > 0 ? (entry.price ?? 0) : (entry.effectivePrice ?? 0);
        return price > 0 ? formatUnitPrice(price, props.item.currency, 4) : '—';
    }

    function formatEntryDate(value: string): string {
        try {
            return new Intl.DateTimeFormat(resolvedLocale(), {
                year: 'numeric',
                month: '2-digit',
                day: '2-digit',
            }).format(new Date(`${toInputDate(value)}T00:00:00`));
        } catch {
            return value.substring(0, 10);
        }
    }

    function pnlTone(value: number | null): string {
        if (value === null) return '';
        return value > 0 ? 'tone-rise' : value < 0 ? 'tone-fall' : '';
    }
</script>

<template>
    <Dialog
        v-model:visible="visibleProxy"
        modal
        :closable="false"
        :header="dialogHeader"
        class="desk-dialog dca-ledger-dialog"
    >
        <div class="dca-ledger-shell">
            <section v-if="entryExpanded" class="dca-entry-card" aria-labelledby="dca-entry-title">
                <div class="dca-entry-heading">
                    <div>
                        <h3 id="dca-entry-title">
                            {{
                                editingId
                                    ? t('dialogs.dcaDetail.quick.editTitle')
                                    : t('dialogs.dcaDetail.quick.addTitle')
                            }}
                        </h3>
                        <p>{{ t('dialogs.dcaDetail.quick.description') }}</p>
                    </div>
                    <div class="dca-entry-tools">
                        <button class="dca-collapse-entry" type="button" :disabled="saving" @click="collapseEntry">
                            <i class="pi pi-chevron-up" />
                            {{ t('dialogs.dcaDetail.quick.collapse') }}
                        </button>
                        <div class="dca-mode-switch" :aria-label="t('dialogs.dcaDetail.quick.modeLabel')">
                            <button
                                type="button"
                                :class="{ active: entryMode === 'price' }"
                                @click="entryMode = 'price'"
                            >
                                {{ t('dialogs.dcaDetail.quick.modePrice') }}
                            </button>
                            <button
                                type="button"
                                :class="{ active: entryMode === 'shares' }"
                                @click="entryMode = 'shares'"
                            >
                                {{ t('dialogs.dcaDetail.quick.modeShares') }}
                            </button>
                        </div>
                    </div>
                </div>

                <div class="dca-primary-fields">
                    <label class="dca-field">
                        <span>{{ t('dialogs.dcaDetail.quick.date') }}</span>
                        <input v-model="draft.date" type="date" class="dca-date-input" />
                    </label>
                    <label class="dca-field">
                        <span>{{ t('dialogs.dcaDetail.quick.amount') }}</span>
                        <InputNumber
                            v-model="draft.amount"
                            :min="0"
                            :step="100"
                            :max-fraction-digits="4"
                            fluid
                            autofocus
                        />
                        <small>{{ item?.currency }}</small>
                    </label>
                    <label v-if="entryMode === 'price'" class="dca-field">
                        <span>{{ t('dialogs.dcaDetail.quick.price') }}</span>
                        <InputNumber v-model="draft.price" :min="0" :step="0.01" :max-fraction-digits="4" fluid />
                        <small>{{ item?.currency }}</small>
                    </label>
                    <label v-else class="dca-field">
                        <span>{{ t('dialogs.dcaDetail.quick.shares') }}</span>
                        <InputNumber v-model="draft.shares" :min="0" :step="0.001" :max-fraction-digits="4" fluid />
                        <small>{{ t('dialogs.dcaDetail.quick.shareUnit') }}</small>
                    </label>
                    <div class="dca-derived-result" aria-live="polite">
                        <span>{{
                            entryMode === 'price'
                                ? t('dialogs.dcaDetail.quick.derivedShares')
                                : t('dialogs.dcaDetail.quick.derivedPrice')
                        }}</span>
                        <strong v-if="entryMode === 'price'">
                            {{ derivedShares > 0 ? formatNumber(derivedShares, 4) : '—' }}
                            <small>{{ t('dialogs.dcaDetail.quick.shareUnit') }}</small>
                        </strong>
                        <strong v-else>
                            {{ derivedPrice > 0 ? formatUnitPrice(derivedPrice, item?.currency ?? '', 4) : '—' }}
                        </strong>
                    </div>
                </div>

                <button class="dca-optional-toggle" type="button" @click="showOptional = !showOptional">
                    <i :class="showOptional ? 'pi pi-chevron-up' : 'pi pi-chevron-down'" />
                    {{
                        showOptional
                            ? t('dialogs.dcaDetail.quick.lessOptions')
                            : t('dialogs.dcaDetail.quick.moreOptions')
                    }}
                </button>

                <div v-if="showOptional" class="dca-optional-fields">
                    <label class="dca-field">
                        <span>{{ t('dialogs.dcaDetail.quick.fee') }}</span>
                        <InputNumber v-model="draft.fee" :min="0" :step="0.01" :max-fraction-digits="4" fluid />
                        <small>{{ item?.currency }}</small>
                    </label>
                    <label class="dca-field dca-note-field">
                        <span>{{ t('dialogs.dcaDetail.quick.note') }}</span>
                        <InputText v-model="draft.note" fluid />
                    </label>
                </div>

                <div class="dca-entry-actions">
                    <div class="dca-entry-secondary-actions">
                        <Button
                            v-if="sortedEntries.length && !editingId"
                            text
                            size="small"
                            icon="pi pi-copy"
                            :label="t('dialogs.dcaDetail.quick.copyLast')"
                            @click="copyLastEntry"
                        />
                        <Button
                            v-if="editingId"
                            text
                            size="small"
                            :label="t('dialogs.dcaDetail.quick.cancelEdit')"
                            @click="resetDraft"
                        />
                    </div>
                    <Button
                        size="small"
                        icon="pi pi-check"
                        :label="
                            editingId ? t('dialogs.dcaDetail.quick.saveEdit') : t('dialogs.dcaDetail.quick.saveEntry')
                        "
                        :disabled="!canSubmit"
                        :loading="saving"
                        @click="submitDraft"
                    />
                </div>
            </section>

            <section v-if="summary && summary.count > 0" class="dca-summary-bar" aria-label="DCA summary">
                <div class="dca-summary-cell">
                    <span>{{ t('dialogs.dcaDetail.summary.count') }}</span>
                    <strong>{{ t('dialogs.dcaDetail.summary.countValue', { count: summary.count }) }}</strong>
                </div>
                <div class="dca-summary-cell">
                    <span>{{ t('dialogs.dcaDetail.summary.totalInvested') }}</span>
                    <strong>{{ formatUnitPrice(summary.totalAmount, item?.currency ?? '', 4) }}</strong>
                </div>
                <div class="dca-summary-cell">
                    <span>{{ t('dialogs.dcaDetail.summary.totalShares') }}</span>
                    <strong>{{ formatNumber(summary.totalShares, 4) }}</strong>
                </div>
                <div class="dca-summary-cell">
                    <span>{{ t('dialogs.dcaDetail.summary.weightedAvgPrice') }}</span>
                    <strong>{{ formatUnitPrice(summary.averageCost, item?.currency ?? '') }}</strong>
                </div>
                <div v-if="summary.hasCurrentPrice" class="dca-summary-cell">
                    <span>{{ t('dialogs.dcaDetail.summary.currentValue') }}</span>
                    <strong>{{ formatUnitPrice(summary.currentValue, item?.currency ?? '') }}</strong>
                </div>
                <div v-if="summary.hasCurrentPrice" class="dca-summary-cell">
                    <span>{{ t('dialogs.dcaDetail.summary.positionPnL') }}</span>
                    <strong :class="pnlTone(summary.pnl)">
                        {{ formatMoney(summary.pnl ?? 0, true) }} <small>{{ formatPercent(summary.pnlPct) }}</small>
                    </strong>
                </div>
            </section>

            <section class="dca-history" aria-labelledby="dca-history-title">
                <div class="dca-history-heading">
                    <h3 id="dca-history-title">{{ t('dialogs.dcaDetail.history.title') }}</h3>
                    <Button
                        v-if="!entryExpanded"
                        size="small"
                        icon="pi pi-plus"
                        :label="t('dialogs.dcaDetail.quick.addTitle')"
                        :disabled="saving"
                        @click="entryExpanded = true"
                    />
                </div>

                <div v-if="sortedEntries.length" class="dca-detail-table">
                    <div class="dca-detail-head">
                        <span>{{ t('dialogs.dcaDetail.table.date') }}</span>
                        <span>{{ t('dialogs.dcaDetail.table.investedAmount') }}</span>
                        <span>{{ t('dialogs.dcaDetail.table.boughtShares') }}</span>
                        <span>{{ t('dialogs.dcaDetail.table.buyPrice') }}</span>
                        <span>{{ t('dialogs.dcaDetail.table.fee') }}</span>
                        <span>{{ t('dialogs.dcaDetail.table.note') }}</span>
                        <span>{{ t('dialogs.dcaDetail.table.actions') }}</span>
                    </div>
                    <div
                        v-for="entry in sortedEntries"
                        :key="entry.id"
                        class="dca-detail-row"
                        :class="{ editing: editingId === entry.id, confirming: pendingDeleteId === entry.id }"
                    >
                        <span>{{ formatEntryDate(entry.date) }}</span>
                        <strong>{{ formatUnitPrice(entry.amount ?? 0, item?.currency ?? '', 4) }}</strong>
                        <span>{{ formatNumber(entry.shares ?? 0, 4) }}</span>
                        <span>{{ buyPrice(entry) }}</span>
                        <span>{{
                            (entry.fee ?? 0) > 0 ? formatUnitPrice(entry.fee ?? 0, item?.currency ?? '') : '—'
                        }}</span>
                        <span class="dca-note-col">{{ entry.note || '—' }}</span>
                        <div v-if="pendingDeleteId !== entry.id" class="dca-row-actions">
                            <Button
                                text
                                rounded
                                size="small"
                                icon="pi pi-pencil"
                                :aria-label="t('dialogs.dcaDetail.history.edit')"
                                :disabled="saving"
                                @click="startEdit(entry)"
                            />
                            <Button
                                text
                                rounded
                                size="small"
                                severity="danger"
                                icon="pi pi-trash"
                                :aria-label="t('dialogs.dcaDetail.history.delete')"
                                :disabled="saving"
                                @click="pendingDeleteId = entry.id"
                            />
                        </div>
                        <div v-else class="dca-delete-confirm">
                            <Button
                                size="small"
                                severity="danger"
                                :label="t('dialogs.dcaDetail.history.confirmDelete')"
                                :loading="saving"
                                @click="confirmDelete(entry.id)"
                            />
                            <Button text size="small" :label="t('common.cancel')" @click="pendingDeleteId = ''" />
                        </div>
                    </div>
                </div>

                <div v-else class="dca-empty-state">
                    <i class="pi pi-calendar-plus" />
                    <strong>{{ t('dialogs.dcaDetail.history.emptyTitle') }}</strong>
                    <span>{{ t('dialogs.dcaDetail.history.emptyDescription') }}</span>
                </div>
            </section>
        </div>

        <template #footer>
            <Button size="small" text :label="t('common.close')" :disabled="saving" @click="visibleProxy = false" />
        </template>
    </Dialog>
</template>

<style scoped>
    :global(.p-dialog.dca-ledger-dialog) {
        width: min(65rem, calc(100vw - 3rem));
        height: min(52rem, calc(100dvh - 2rem));
    }

    :global(.p-dialog.dca-ledger-dialog .p-dialog-content) {
        display: flex;
        min-height: 0;
        overflow: hidden;
    }

    .dca-ledger-shell {
        display: flex;
        flex: 1;
        flex-direction: column;
        gap: 12px;
        min-height: 0;
    }
    .dca-entry-card {
        padding: 14px;
        background: color-mix(in srgb, var(--accent-soft) 58%, var(--panel-strong));
        border: 1px solid color-mix(in srgb, var(--accent) 22%, var(--border));
        border-radius: var(--radius-panel);
        box-shadow: inset 0 1px 0 color-mix(in srgb, white 35%, transparent);
    }
    .dca-entry-tools {
        display: flex;
        align-items: center;
        flex: none;
        gap: 10px;
    }
    .dca-collapse-entry {
        display: inline-flex;
        align-items: center;
        gap: 5px;
        padding: 5px 2px;
        color: var(--muted);
        background: transparent;
        border: 0;
        font: 500 11px/1 var(--font-ui);
        cursor: pointer;
    }
    .dca-collapse-entry:hover {
        color: var(--accent);
    }
    .dca-collapse-entry:disabled {
        cursor: default;
        opacity: 0.5;
    }
    .dca-collapse-entry i {
        font-size: 9px;
    }
    .dca-entry-heading,
    .dca-history-heading,
    .dca-entry-actions {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 16px;
    }
    h3 {
        margin: 0;
        font: 600 17px/1.2 var(--font-display);
        color: var(--ink);
        letter-spacing: -0.01em;
    }
    .dca-entry-heading p {
        max-width: 610px;
        margin: 5px 0 0;
        font: 400 12px/1.55 var(--font-ui);
        color: var(--muted);
    }
    .dca-mode-switch {
        display: inline-grid;
        grid-template-columns: 1fr 1fr;
        flex: none;
        padding: 3px;
        background: var(--panel-soft);
        border: 1px solid var(--border);
        border-radius: var(--radius-control);
    }
    .dca-mode-switch button {
        padding: 6px 10px;
        color: var(--muted);
        background: transparent;
        border: 0;
        border-radius: var(--radius-micro);
        font: 600 11px/1 var(--font-ui);
        cursor: pointer;
        transition:
            color 0.18s ease,
            background 0.18s ease,
            transform 0.18s ease;
    }
    .dca-mode-switch button:hover {
        color: var(--ink);
    }
    .dca-mode-switch button:active {
        transform: translateY(1px);
    }
    .dca-mode-switch button:focus-visible,
    .dca-optional-toggle:focus-visible,
    .dca-collapse-entry:focus-visible {
        outline: 2px solid var(--accent);
        outline-offset: 2px;
    }
    .dca-mode-switch button.active {
        color: var(--accent);
        background: var(--panel-strong);
        box-shadow: 0 1px 4px color-mix(in srgb, var(--accent) 12%, transparent);
    }
    .dca-primary-fields {
        display: grid;
        grid-template-columns: 1.05fr 1fr 1fr 1.15fr;
        gap: 10px;
        align-items: end;
        margin-top: 14px;
    }
    .dca-field {
        position: relative;
        display: flex;
        flex-direction: column;
        gap: 6px;
        min-width: 0;
        color: var(--ink);
    }
    .dca-field > span {
        font: 600 11px/1 var(--font-ui);
        color: var(--muted);
    }
    .dca-field > small {
        position: absolute;
        right: 9px;
        bottom: 10px;
        z-index: 1;
        font: 500 9px/1 var(--font-ui);
        color: var(--muted);
        pointer-events: none;
    }
    .dca-date-input {
        width: 100%;
        height: 34px;
        padding: 0 9px;
        box-sizing: border-box;
        color: var(--ink);
        background: var(--control-bg);
        border: 1px solid var(--border-strong);
        border-radius: var(--radius-control);
        font: 500 12px/1 var(--font-ui);
        color-scheme: light dark;
        outline: none;
    }
    .dca-date-input:focus {
        border-color: var(--accent);
        box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 14%, transparent);
    }
    .dca-field :deep(.p-inputnumber-input),
    .dca-field :deep(.p-inputtext) {
        height: 34px;
        padding-right: 44px;
        font-size: 12px;
        font-variant-numeric: tabular-nums;
    }
    .dca-derived-result {
        display: flex;
        flex-direction: column;
        justify-content: center;
        gap: 5px;
        min-height: 34px;
        padding: 7px 11px;
        box-sizing: border-box;
        background: var(--panel-strong);
        border: 1px solid color-mix(in srgb, var(--accent) 18%, var(--border));
        border-radius: var(--radius-control);
    }
    .dca-derived-result > span {
        font: 500 9px/1 var(--font-ui);
        color: var(--muted);
    }
    .dca-derived-result strong {
        font: 600 13px/1 var(--font-display);
        color: var(--accent);
        font-variant-numeric: tabular-nums;
    }
    .dca-derived-result small {
        font: 500 9px/1 var(--font-ui);
    }
    .dca-optional-toggle {
        display: inline-flex;
        align-items: center;
        gap: 6px;
        margin-top: 12px;
        padding: 3px 0;
        color: var(--muted);
        background: transparent;
        border: 0;
        font: 500 11px/1 var(--font-ui);
        cursor: pointer;
    }
    .dca-optional-toggle:hover {
        color: var(--accent);
    }
    .dca-optional-toggle i {
        font-size: 9px;
    }
    .dca-optional-fields {
        display: grid;
        grid-template-columns: 1fr 2.1fr;
        gap: 10px;
        margin-top: 10px;
    }
    .dca-entry-actions {
        margin-top: 14px;
        padding-top: 12px;
        border-top: 1px solid color-mix(in srgb, var(--accent) 14%, var(--border));
    }
    .dca-entry-secondary-actions {
        min-height: 30px;
    }
    .dca-summary-bar {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(125px, 1fr));
        gap: 1px;
        overflow: hidden;
        background: var(--border);
        border: 1px solid var(--border);
        border-radius: var(--radius-control);
    }
    .dca-summary-cell {
        display: flex;
        flex-direction: column;
        gap: 5px;
        padding: 10px 12px;
        background: var(--panel-strong);
    }
    .dca-summary-cell > span {
        font: 500 10px/1 var(--font-ui);
        color: var(--muted);
    }
    .dca-summary-cell > strong {
        font: 500 13px/1.2 var(--font-display);
        color: var(--ink);
        white-space: nowrap;
        font-variant-numeric: tabular-nums;
    }
    .dca-summary-cell strong small {
        margin-left: 4px;
        font: 400 10px/1 var(--font-ui);
    }
    .dca-summary-cell .tone-rise {
        color: var(--rise);
    }
    .dca-summary-cell .tone-fall {
        color: var(--fall);
    }
    .dca-history {
        display: flex;
        flex: 1;
        flex-direction: column;
        min-height: 0;
    }
    .dca-history-heading {
        flex: none;
        min-height: 30px;
        margin-bottom: 10px;
    }
    .dca-detail-table {
        flex: 1;
        min-height: 0;
        overflow: auto;
        overscroll-behavior: contain;
        border: 1px solid var(--border);
        border-radius: var(--radius-control);
        scrollbar-gutter: stable;
    }
    .dca-detail-head,
    .dca-detail-row {
        display: grid;
        grid-template-columns: 116px 130px 112px 118px 82px minmax(110px, 1fr) 124px;
        align-items: center;
        gap: 0;
    }
    .dca-detail-head {
        position: sticky;
        z-index: 1;
        top: 0;
        padding: 7px 4px;
        color: var(--muted);
        background: var(--panel-soft);
        border-bottom: 1px solid var(--border);
        box-shadow: 0 1px 0 color-mix(in srgb, var(--border) 80%, transparent);
        font: 600 10px/1 var(--font-ui);
    }
    .dca-detail-head > span,
    .dca-detail-row > span,
    .dca-detail-row > strong {
        min-width: 0;
        padding: 0 8px;
        text-align: left;
    }
    .dca-detail-row {
        min-height: 42px;
        color: var(--ink);
        border-bottom: 1px solid var(--border);
        font: 500 12px/1.25 var(--font-display);
        font-variant-numeric: tabular-nums;
        transition: background 0.18s ease;
    }
    .dca-detail-row:last-child {
        border-bottom: 0;
    }
    .dca-detail-row:hover,
    .dca-detail-row.editing {
        background: var(--selection-bg);
    }
    .dca-detail-row.confirming {
        background: color-mix(in srgb, var(--fall) 6%, var(--panel-strong));
    }
    .dca-note-col {
        overflow: hidden;
        color: var(--muted);
        font-family: var(--font-ui);
        font-size: 11px;
        font-weight: 400;
        text-overflow: ellipsis;
        white-space: nowrap;
    }
    .dca-row-actions,
    .dca-delete-confirm {
        display: flex;
        align-items: center;
        justify-content: flex-start;
        gap: 2px;
        padding-right: 4px;
    }
    .dca-delete-confirm {
        gap: 0;
    }
    .dca-delete-confirm :deep(.p-button) {
        padding-inline: 6px;
        font-size: 10px;
    }
    .dca-empty-state {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: 6px;
        padding: 28px 16px 22px;
        color: var(--muted);
        background: var(--panel-soft);
        border: 1px dashed var(--border-strong);
        border-radius: var(--radius-control);
        text-align: center;
    }
    .dca-empty-state i {
        margin-bottom: 3px;
        color: var(--accent);
        font-size: 20px;
    }
    .dca-empty-state strong {
        color: var(--ink);
        font: 600 13px/1.2 var(--font-ui);
    }
    .dca-empty-state span {
        font: 400 11px/1.5 var(--font-ui);
    }
    @media (max-width: 900px) {
        :global(.p-dialog.dca-ledger-dialog) {
            width: calc(100vw - 2rem);
        }
        .dca-primary-fields {
            grid-template-columns: repeat(2, 1fr);
        }
        .dca-detail-head,
        .dca-detail-row {
            min-width: 880px;
        }
    }
</style>
