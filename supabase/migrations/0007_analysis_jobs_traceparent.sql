-- Carry the trace across the queue.
--
-- A queue is a gap in a trace: the request that enqueues the work finishes in
-- under a second, and the worker picks the row up later in a different process
-- with no context to inherit. Storing the W3C traceparent on the row is what
-- closes that gap, so an analysis reads as one trace from the click to the
-- model call instead of two unrelated ones.
--
-- Text rather than a composite type: it is an opaque header value defined by
-- someone else's specification, and parsing it here would only create a second
-- place that has to agree with them.
--
-- Nullable on purpose. Rows enqueued before this column existed have no trace,
-- and a deployment with tracing switched off never writes one; both are normal
-- and neither should stop a job from running.
alter table public.analysis_jobs
  add column if not exists traceparent text;

comment on column public.analysis_jobs.traceparent is
  'W3C trace context of the request that enqueued this job, so the worker can continue its trace.';
