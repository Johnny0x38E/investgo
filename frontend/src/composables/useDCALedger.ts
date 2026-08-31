import { ref } from 'vue';

import { api } from '../api';
import { mapItemToForm, serialiseItemForm } from '../forms';
import { translate } from '../i18n';
import type { DCAEntryRow, StateSnapshot, StatusTone, WatchlistItem } from '../types';

type StatusReporter = (message: string, tone: StatusTone) => void;

export function useDCALedger(
    applySnapshot: (snapshot: StateSnapshot) => void,
    clearHistoryCache: () => void,
    setStatus: StatusReporter,
) {
    const dcaLedgerVisible = ref(false);
    const dcaLedgerItem = ref<WatchlistItem | null>(null);
    const savingDCAEntries = ref(false);

    function openDCALedger(item: WatchlistItem): void {
        dcaLedgerItem.value = item;
        dcaLedgerVisible.value = true;
    }

    async function saveDCAEntries(entries: DCAEntryRow[]): Promise<void> {
        const item = dcaLedgerItem.value;
        if (!item || savingDCAEntries.value) {
            return;
        }

        savingDCAEntries.value = true;
        try {
            const form = mapItemToForm(item);
            form.dcaEntries = entries;
            const snapshot = await api<StateSnapshot>(`/api/items/${item.id}`, {
                method: 'PUT',
                body: JSON.stringify(serialiseItemForm(form)),
            });

            clearHistoryCache();
            applySnapshot(snapshot);
            dcaLedgerItem.value = snapshot.items.find((candidate) => candidate.id === item.id) ?? null;
            setStatus(translate('app.dcaRecordsSaved'), 'success');
        } catch (error) {
            setStatus(error instanceof Error ? error.message : translate('app.dcaRecordsSaveFailed'), 'error');
        } finally {
            savingDCAEntries.value = false;
        }
    }

    return {
        dcaLedgerVisible,
        dcaLedgerItem,
        savingDCAEntries,
        openDCALedger,
        saveDCAEntries,
    };
}
