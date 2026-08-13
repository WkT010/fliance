import { api } from './client';
import type { KycStatusResponse } from '@/types';

export interface KycSubmitReq {
  full_name: string;
  id_number: string;
  /** Base64 png/jpeg image (data URLs accepted by the backend). */
  doc_front: string;
  /** Base64 png/jpeg image (data URLs accepted by the backend). */
  doc_back: string;
}

export interface KycSubmitResult {
  id: string;
  status: string;
  /** Unix nanoseconds. */
  submitted_at: number;
}

/** POST /kyc/submit — submit identity documents for review. */
export async function submitKyc(body: KycSubmitReq): Promise<KycSubmitResult> {
  const res = await api.post('/kyc/submit', body);
  return res.data;
}

/** GET /kyc/status — current KYC level + latest submission. */
export async function getKycStatus(): Promise<KycStatusResponse> {
  const res = await api.get('/kyc/status');
  return res.data;
}
