import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Layout } from '@/components/Layout';
import { Card } from '@/components/common/Card';
import { Button } from '@/components/common/Button';
import { Input } from '@/components/common/Input';
import { Badge } from '@/components/common/Badge';
import { submitKyc, getKycStatus } from '@/api/kyc';
import { useFetch } from '@/hooks/useFetch';
import { toast } from '@/store/toastStore';
import { formatDate, cls } from '@/utils/format';

const MAX_DOC_BYTES = 5 * 1024 * 1024;
const ACCEPTED_TYPES = ['image/jpeg', 'image/png'];

/** Converts a Unix-nanosecond timestamp for display (formatDate auto-detects ns). */
const nanoDate = (ts?: number) => (ts ? formatDate(ts) : '--');

/**
 * One identity-document upload slot: file picker → client-side validation
 * (≤5MB, jpg/png only) → base64 data URL kept in state. The backend accepts
 * data URLs directly.
 */
function DocUpload({
  label,
  value,
  onChange,
  errorText,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  errorText: { tooLarge: string; badType: string };
}) {
  const { t } = useTranslation();
  const inputRef = useRef<HTMLInputElement>(null);
  const [error, setError] = useState('');

  const pick = (file: File | undefined) => {
    if (!file) return;
    if (file.size > MAX_DOC_BYTES) {
      setError(errorText.tooLarge);
      onChange('');
      return;
    }
    if (!ACCEPTED_TYPES.includes(file.type)) {
      setError(errorText.badType);
      onChange('');
      return;
    }
    setError('');
    const reader = new FileReader();
    reader.onload = () => onChange(typeof reader.result === 'string' ? reader.result : '');
    reader.readAsDataURL(file);
  };

  return (
    <div>
      <div className="mb-1 block text-xs font-medium text-nexa-300">{label}</div>
      <input
        ref={inputRef}
        type="file"
        accept="image/jpeg,image/png"
        className="hidden"
        onChange={(e) => pick(e.target.files?.[0])}
      />
      <button
        type="button"
        onClick={() => inputRef.current?.click()}
        className={cls(
          'flex h-32 w-full items-center justify-center overflow-hidden rounded-lg border border-dashed transition-colors',
          value ? 'border-accent/50 bg-nexa-900' : 'border-nexa-600 bg-nexa-900 hover:border-accent'
        )}
      >
        {value ? (
          <img src={value} alt={label} className="h-full w-full object-contain" />
        ) : (
          <span className="text-xs text-nexa-500">{t('kyc.uploadHint')}</span>
        )}
      </button>
      {error && <p className="mt-1 text-xs text-down">{error}</p>}
    </div>
  );
}

export function KycPage() {
  const { t } = useTranslation();
  const { data: status, refetch } = useFetch(getKycStatus, []);

  const [fullName, setFullName] = useState('');
  const [idNumber, setIdNumber] = useState('');
  const [docFront, setDocFront] = useState('');
  const [docBack, setDocBack] = useState('');
  const [busy, setBusy] = useState(false);

  const sub = status?.submission ?? null;
  const approved = sub?.status === 'approved' || (status?.kyc_level ?? 0) > 0;
  const pending = sub?.status === 'pending';
  const showForm = !approved && !pending; // never submitted, or rejected → resubmit

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (busy) return;
    if (!docFront || !docBack) {
      toast.error(t('kyc.docsRequired'));
      return;
    }
    setBusy(true);
    try {
      await submitKyc({ full_name: fullName, id_number: idNumber, doc_front: docFront, doc_back: docBack });
      toast.success(t('kyc.submitSuccess'));
      setFullName('');
      setIdNumber('');
      setDocFront('');
      setDocBack('');
      refetch();
    } catch (err: unknown) {
      const msg = (err as { response?: { data?: { error?: string } } })?.response?.data?.error;
      toast.error(msg || (err instanceof Error ? err.message : t('kyc.submitFailed')));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Layout>
      <div className="mx-auto max-w-2xl p-4 pb-10">
        <h1 className="mb-1 text-xl font-semibold text-nexa-100">{t('kyc.title')}</h1>
        <p className="mb-6 text-sm text-nexa-400">{t('kyc.subtitle')}</p>

        {/* Approved */}
        {approved && (
          <Card>
            <div className="flex flex-col items-center gap-3 p-8 text-center">
              <span className="flex h-12 w-12 items-center justify-center rounded-full bg-up/15 text-up">
                <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
                  <path d="M5 13l4 4L19 7" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
              </span>
              <div className="text-lg font-semibold text-nexa-100">{t('kyc.statusApproved')}</div>
              <p className="text-sm text-nexa-400">{t('kyc.statusApprovedDesc')}</p>
              {sub?.reviewed_at ? (
                <div className="text-xs text-nexa-500">{t('kyc.reviewedAt')}: {nanoDate(sub.reviewed_at)}</div>
              ) : null}
            </div>
          </Card>
        )}

        {/* Pending */}
        {pending && (
          <Card>
            <div className="flex flex-col items-center gap-3 p-8 text-center">
              <span className="flex h-12 w-12 items-center justify-center rounded-full bg-accent/15 text-accent">
                <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6">
                  <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="2" />
                  <path d="M12 7v5l3 3" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
              </span>
              <div className="text-lg font-semibold text-nexa-100">{t('kyc.statusPending')}</div>
              <p className="text-sm text-nexa-400">{t('kyc.statusPendingDesc')}</p>
              <div className="text-xs text-nexa-500">{t('kyc.submittedAt')}: {nanoDate(sub?.submitted_at)}</div>
            </div>
          </Card>
        )}

        {/* Rejected banner + resubmission form */}
        {showForm && (
          <>
            {sub?.status === 'rejected' && (
              <div className="mb-4 rounded-lg border border-down/30 bg-down/10 p-4">
                <div className="flex items-center gap-2">
                  <Badge color="down">{t('kyc.statusRejected')}</Badge>
                  <span className="text-xs text-nexa-500">{t('kyc.reviewedAt')}: {nanoDate(sub.reviewed_at)}</span>
                </div>
                <p className="mt-2 text-sm text-nexa-200">
                  {t('kyc.rejectReason')}: {sub.reject_reason || '--'}
                </p>
                <p className="mt-1 text-xs text-nexa-500">{t('kyc.resubmitHint')}</p>
              </div>
            )}

            <Card title={t('kyc.formTitle')}>
              <form onSubmit={submit} className="space-y-4 p-4">
                <Input
                  label={t('kyc.fullName')}
                  value={fullName}
                  onChange={(e) => setFullName(e.target.value)}
                  required
                />
                <Input
                  label={t('kyc.idNumber')}
                  value={idNumber}
                  onChange={(e) => setIdNumber(e.target.value)}
                  required
                />
                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <DocUpload
                    label={t('kyc.docFront')}
                    value={docFront}
                    onChange={setDocFront}
                    errorText={{ tooLarge: t('kyc.fileTooLarge'), badType: t('kyc.fileTypeInvalid') }}
                  />
                  <DocUpload
                    label={t('kyc.docBack')}
                    value={docBack}
                    onChange={setDocBack}
                    errorText={{ tooLarge: t('kyc.fileTooLarge'), badType: t('kyc.fileTypeInvalid') }}
                  />
                </div>
                <p className="text-xs text-nexa-500">{t('kyc.uploadRules')}</p>
                <Button type="submit" isLoading={busy} block>
                  {t('kyc.submit')}
                </Button>
              </form>
            </Card>
          </>
        )}
      </div>
    </Layout>
  );
}
