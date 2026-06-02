-- Bilingual analysis content. Only needed for databases created before this
-- change; on a fresh install 0001 already creates these columns as jsonb, and
-- the guards below make this migration a no-op.
--
-- Legacy single-language values are wrapped into { "en": x, "es": x } so existing
-- history stays readable (it just shows the same text in both languages).

do $$
begin
  if (
    select data_type
    from information_schema.columns
    where table_schema = 'public' and table_name = 'analyses' and column_name = 'summary'
  ) = 'text' then
    alter table public.analyses
      alter column summary type jsonb
      using jsonb_build_object('en', summary, 'es', summary);
  end if;

  if (
    select data_type
    from information_schema.columns
    where table_schema = 'public' and table_name = 'analyses' and column_name = 'matched_skills'
  ) = 'ARRAY' then
    alter table public.analyses
      alter column matched_skills drop default,
      alter column matched_skills type jsonb
        using jsonb_build_object('en', to_jsonb(matched_skills), 'es', to_jsonb(matched_skills)),
      alter column matched_skills set default '{"en": [], "es": []}'::jsonb;
  end if;

  if (
    select data_type
    from information_schema.columns
    where table_schema = 'public' and table_name = 'analyses' and column_name = 'missing_skills'
  ) = 'ARRAY' then
    alter table public.analyses
      alter column missing_skills drop default,
      alter column missing_skills type jsonb
        using jsonb_build_object('en', to_jsonb(missing_skills), 'es', to_jsonb(missing_skills)),
      alter column missing_skills set default '{"en": [], "es": []}'::jsonb;
  end if;
end $$;

-- Wrap legacy suggestions ([{title, detail, priority}]) into the bilingual shape.
-- Guarded by the title type so it only touches rows still in the old format.
update public.analyses
set suggestions = coalesce((
  select jsonb_agg(
    jsonb_build_object(
      'priority', item ->> 'priority',
      'title', jsonb_build_object('en', item -> 'title', 'es', item -> 'title'),
      'detail', jsonb_build_object('en', item -> 'detail', 'es', item -> 'detail')
    )
  )
  from jsonb_array_elements(suggestions) as item
), '[]'::jsonb)
where jsonb_typeof(suggestions) = 'array'
  and jsonb_array_length(suggestions) > 0
  and jsonb_typeof(suggestions -> 0 -> 'title') = 'string';
