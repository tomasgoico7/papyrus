import type { SupabaseClient } from "@supabase/supabase-js";

const BUCKET = "cvs";
const DOWNLOAD_URL_TTL_SECONDS = 60;

export interface StoredCv {
  id: string;
  storagePath: string;
}

export interface StoredCvSummary {
  id: string;
  filename: string;
  storagePath: string;
  createdAt: string;
}

const REUSABLE_CV_LIMIT = 20;

export async function uploadCv(
  supabase: SupabaseClient,
  userId: string,
  file: File,
): Promise<StoredCv> {
  const id = crypto.randomUUID();
  const storagePath = `${userId}/${id}.pdf`;

  const { error: uploadError } = await supabase.storage
    .from(BUCKET)
    .upload(storagePath, file, {
      contentType: "application/pdf",
      upsert: false,
    });
  if (uploadError) {
    throw new Error(`Failed to upload CV: ${uploadError.message}`);
  }

  const { data, error } = await supabase
    .from("cvs")
    .insert({
      id,
      user_id: userId,
      filename: file.name,
      byte_size: file.size,
      storage_path: storagePath,
    })
    .select("id, storage_path")
    .single();
  if (error) {
    await supabase.storage.from(BUCKET).remove([storagePath]);
    throw new Error(`Failed to record CV: ${error.message}`);
  }

  return { id: data.id as string, storagePath: data.storage_path as string };
}

interface CvRow {
  id: string;
  filename: string;
  storage_path: string;
  created_at: string;
}

export async function listCvs(
  supabase: SupabaseClient,
  userId: string,
): Promise<StoredCvSummary[]> {
  const { data, error } = await supabase
    .from("cvs")
    .select("id, filename, storage_path, created_at")
    .eq("user_id", userId)
    .order("created_at", { ascending: false });
  if (error) {
    throw new Error(`Failed to load CVs: ${error.message}`);
  }

  // Each analysis stores its own copy, so de-dupe by filename to keep the
  // picker short.
  const seen = new Set<string>();
  const unique: StoredCvSummary[] = [];
  for (const row of data as CvRow[]) {
    if (seen.has(row.filename)) continue;
    seen.add(row.filename);
    unique.push({
      id: row.id,
      filename: row.filename,
      storagePath: row.storage_path,
      createdAt: row.created_at,
    });
    if (unique.length >= REUSABLE_CV_LIMIT) break;
  }
  return unique;
}

export async function downloadStoredCv(
  supabase: SupabaseClient,
  storagePath: string,
): Promise<Blob> {
  const { data, error } = await supabase.storage
    .from(BUCKET)
    .download(storagePath);
  if (error || !data) {
    throw new Error(
      `Failed to download CV: ${error?.message ?? "unknown error"}`,
    );
  }
  return data;
}

export async function removeStoredCv(
  supabase: SupabaseClient,
  storagePath: string,
): Promise<void> {
  await supabase.storage.from(BUCKET).remove([storagePath]);
  await supabase.from("cvs").delete().eq("storage_path", storagePath);
}

export async function createCvDownloadUrl(
  supabase: SupabaseClient,
  storagePath: string,
): Promise<string> {
  const { data, error } = await supabase.storage
    .from(BUCKET)
    .createSignedUrl(storagePath, DOWNLOAD_URL_TTL_SECONDS);
  if (error || !data) {
    throw new Error(
      `Failed to create download link: ${error?.message ?? "unknown error"}`,
    );
  }
  return data.signedUrl;
}
