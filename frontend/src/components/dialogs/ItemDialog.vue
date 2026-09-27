<script setup lang="ts">
    import { computed, onBeforeUnmount, ref, watch } from 'vue';
    import Button from 'primevue/button';
    import Dialog from 'primevue/dialog';
    import InputNumber from 'primevue/inputnumber';
    import InputText from 'primevue/inputtext';
    import Select from 'primevue/select';
    import Textarea from 'primevue/textarea';

    import { ApiAbortError, api } from '../../api';
    import { currencyOptions, getMarketOptions } from '../../constants';
    import { applySymbolLookup } from '../../forms';
    import { useI18n } from '../../i18n';
    import type { ItemFormModel, SymbolLookup } from '../../types';

    const props = defineProps<{
        visible: boolean;
        form: ItemFormModel;
        saving: boolean;
        watchOnly?: boolean;
    }>();

    const emit = defineEmits<{
        (event: 'update:visible', value: boolean): void;
        (event: 'save'): void;
    }>();

    const visibleProxy = computed({
        get: () => props.visible,
        set: (value: boolean) => emit('update:visible', value),
    });

    const { t } = useI18n();
    const marketOptions = computed(() => getMarketOptions());

    // The backend derives position size and cost price from valid DCA records.
    const hasDCA = computed(() => props.form.dcaEntries.some((e) => (e.amount ?? 0) > 0 && (e.shares ?? 0) > 0));

    const lastLookupKey = ref('');
    let lookupTimer = 0;
    let lookupAbort: AbortController | null = null;

    function normalizeLookupKey(symbol: string, market: string): string {
        return `${symbol.trim().toUpperCase().replace(/\s/g, '')}|${market}`;
    }

    function symbolLooksComplete(symbol: string): boolean {
        const value = symbol.trim().toUpperCase().replace(/\s/g, '');
        if (!value) {
            return false;
        }
        if (/^\d{6}(\.(SH|SZ|BJ))?$/.test(value) || /^(SH|SZ|BJ)\d{6}$/.test(value)) {
            return true;
        }
        if (/^(HK)?\d{1,5}\.HK$/.test(value)) {
            return true;
        }
        return /^[A-Z][A-Z0-9.-]{0,9}$/.test(value);
    }

    function cancelLookup(): void {
        window.clearTimeout(lookupTimer);
        lookupTimer = 0;
        lookupAbort?.abort();
        lookupAbort = null;
    }

    function rememberOpenedSymbol(): void {
        lastLookupKey.value = normalizeLookupKey(props.form.symbol, props.form.market);
    }

    async function lookupSymbol(force: boolean): Promise<void> {
        const symbol = props.form.symbol.trim();
        if (!symbol) {
            return;
        }
        if (!force && !symbolLooksComplete(symbol)) {
            return;
        }

        const requestKey = normalizeLookupKey(symbol, props.form.market);
        if (requestKey === lastLookupKey.value) {
            return;
        }

        lookupAbort?.abort();
        const controller = new AbortController();
        lookupAbort = controller;

        try {
            const params = new URLSearchParams({ symbol, market: props.form.market });
            const result = await api<SymbolLookup>(`/api/lookup?${params.toString()}`, {
                timeoutMs: 10000,
                signal: controller.signal,
            });
            lastLookupKey.value = normalizeLookupKey(result.symbol, result.market);
            applySymbolLookup(props.form, result, { watchOnly: Boolean(props.watchOnly) });
            lastLookupKey.value = normalizeLookupKey(props.form.symbol, props.form.market);
        } catch (error) {
            if (error instanceof ApiAbortError) {
                return;
            }
            // Ignore lookup failures; the user can still fill the form manually.
        } finally {
            if (lookupAbort === controller) {
                lookupAbort = null;
            }
        }
    }

    function scheduleLookup(): void {
        window.clearTimeout(lookupTimer);
        lookupTimer = window.setTimeout(() => {
            void lookupSymbol(false);
        }, 400);
    }

    function lookupOnBlur(): void {
        window.clearTimeout(lookupTimer);
        void lookupSymbol(true);
    }

    function markNameCustom(): void {
        const next = props.form.name.trim();
        const fallback = props.form.defaultName.trim();
        props.form.hasCustomName = next !== '' && next !== fallback;
    }

    function resetItemName(): void {
        props.form.name = props.form.defaultName;
        props.form.hasCustomName = false;
    }

    watch(
        () => props.visible,
        (visible) => {
            if (visible) {
                rememberOpenedSymbol();
                return;
            }
            cancelLookup();
        },
        { immediate: true },
    );

    watch(
        () => [props.form.symbol, props.form.market] as const,
        () => {
            if (!props.visible) {
                return;
            }
            scheduleLookup();
        },
    );

    onBeforeUnmount(() => {
        cancelLookup();
    });
</script>

<template>
    <Dialog
        v-model:visible="visibleProxy"
        modal
        :closable="false"
        :header="
            props.watchOnly
                ? t('dialogs.item.watchTitle')
                : form.id
                  ? t('dialogs.item.editTitle')
                  : t('dialogs.item.addTitle')
        "
        :style="{ width: props.watchOnly ? '560px' : '640px' }"
        class="desk-dialog"
    >
        <div class="form-grid">
            <label>
                <span>{{ t('dialogs.item.labels.symbol') }}</span>
                <InputText v-model.trim="form.symbol" @blur="lookupOnBlur" />
            </label>
            <label>
                <span>{{ t('dialogs.item.labels.itemName') }}</span>
                <div class="item-name-field">
                    <InputText v-model.trim="form.name" @update:model-value="markNameCustom" />
                    <Button
                        v-if="form.hasCustomName"
                        size="small"
                        text
                        :label="t('dialogs.item.resetName')"
                        @click="resetItemName"
                    />
                </div>
            </label>
            <label>
                <span>{{ t('dialogs.item.labels.market') }}</span>
                <Select v-model="form.market" :options="marketOptions" option-label="label" option-value="value" />
            </label>
            <label>
                <span>{{ t('dialogs.item.labels.currency') }}</span>
                <Select v-model="form.currency" :options="currencyOptions" option-label="label" option-value="value" />
            </label>

            <label v-if="!props.watchOnly">
                <span>{{ t('dialogs.item.labels.quantity') }}</span>
                <InputNumber
                    v-model="form.quantity"
                    :min="0"
                    :step="0.0001"
                    :max-fraction-digits="4"
                    :readonly="hasDCA"
                    fluid
                />
            </label>

            <label v-if="!props.watchOnly">
                <span>{{ t('dialogs.item.labels.costPrice') }}</span>
                <InputNumber
                    v-model="form.costPrice"
                    :min="0"
                    :step="0.0001"
                    :max-fraction-digits="4"
                    :readonly="hasDCA"
                    fluid
                />
            </label>

            <!-- Acquisition date is only relevant for holdings without DCA entries. -->
            <label v-if="!hasDCA && !props.watchOnly">
                <span>{{ t('dialogs.item.labels.acquiredAt') }}</span>
                <input v-model="form.acquiredAt" type="date" />
            </label>

            <p v-if="hasDCA && !props.watchOnly" class="item-dialog-note">
                <i class="pi pi-info-circle" aria-hidden="true" />
                {{ t('dialogs.item.positionDerived') }}
            </p>

            <label>
                <span>{{ t('dialogs.item.labels.tags') }}</span>
                <InputText v-model.trim="form.tagsText" />
            </label>
            <label class="full-span">
                <span>{{ t('dialogs.item.labels.thesis') }}</span>
                <Textarea v-model="form.thesis" auto-resize rows="5" />
            </label>
        </div>

        <!-- Footer actions -->
        <template #footer>
            <Button size="small" text :label="t('common.cancel')" @click="visibleProxy = false" />
            <Button size="small" :label="t('common.save')" :loading="saving" @click="$emit('save')" />
        </template>
    </Dialog>
</template>

<style scoped>
    .item-dialog-note {
        grid-column: 1 / -1;
        display: flex;
        align-items: center;
        gap: 6px;
        margin: -4px 0 0;
        padding: 7px 10px;
        border-radius: var(--radius-control);
        background: var(--accent-soft);
        color: var(--accent);
        font-size: 11px;
        line-height: 1.5;
    }

    .item-dialog-note i {
        flex: 0 0 auto;
    }

    .item-name-field {
        display: grid;
        grid-template-columns: minmax(0, 1fr) auto;
        gap: 6px;
        align-items: center;
    }
</style>
