import type { SupabaseClient } from "@supabase/supabase-js";

const BUCKET = "cvs";
const DOWNLOAD_URL_TTL_SECONDS = 60;

export interface StoredCv {
  id: string;
  storagePath: string;
}

/**
 * Uploads a CV to private storage and records it in the `cvs` table. The id is
 * minted client-side so it can name both the storage object and the row, and a
 * failed insert rolls back the orphaned object to keep the two in sync.
 */
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

/** Removes a stored CV: the object in Storage and its row in `cvs`. */
export async function removeStoredCv(
  supabase: SupabaseClient,
  storagePath: string,
): Promise<void> {
  await supabase.storage.from(BUCKET).remove([storagePath]);
  await supabase.from("cvs").delete().eq("storage_path", storagePath);
}

/** Mints a short-lived signed URL so a private CV can be opened or downloaded. */
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
