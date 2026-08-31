<script setup lang="ts">
    import { computed } from 'vue';
    import Button from 'primevue/button';
    import Dialog from 'primevue/dialog';
    import InputNumber from 'primevue/inputnumber';
    import InputText from 'primevue/inputtext';
    import Select from 'primevue/select';
    import Textarea from 'primevue/textarea';

    import { currencyOptions, getMarketOptions } from '../../constants';
    import { useI18n } from '../../i18n';
    import type { ItemFormModel } from '../../types';

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
                <InputText v-model.trim="form.symbol" />
            </label>
            <label>
                <span>{{ t('dialogs.item.labels.itemName') }}</span>
                <InputText v-model.trim="form.name" />
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
                <input v-model="form.acquiredAt" type="date" class="item-date-input" />
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

    input[type='date'].item-date-input {
        width: 100%;
        height: 36px;
        padding: 0 12px;
        font: 13px var(--font-ui);
        color: var(--ink);
        background: var(--control-bg);
        border: 1px solid var(--border-strong);
        border-radius: var(--radius-control);
        outline: none;
        box-sizing: border-box;
        color-scheme: light dark;
    }

    input[type='date'].item-date-input:focus {
        border-color: var(--accent);
    }
</style>
