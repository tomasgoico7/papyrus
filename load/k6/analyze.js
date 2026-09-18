// Load profile for POST /analyze.
//
//   k6 run -e TOKEN="<supabase access token>" load/k6/analyze.js
//
// Point BASE_URL at the load-test gateway (port 8081), which talks to the stub
// AI service: a run then costs nothing and measures the gateway rather than the
// model. Against the real stack the numbers are dominated by Gemini and the
// provider quota runs out long before the test does.
//
// TOKEN is a real Supabase access token — the gateway verifies it against the
// project JWKS, and there is no way to mint one offline. Take it from a browser
// session: `(await supabase.auth.getSession()).data.session.access_token`.

import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8081';
const TOKEN = __ENV.TOKEN;
const RATE = Number(__ENV.RATE || 20);
const DURATION = __ENV.DURATION || '60s';

export const options = {
  scenarios: {
    steady: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: 20,
      maxVUs: 200,
    },
  },
  thresholds: {
    // The gateway's own work should stay in single-digit milliseconds; the
    // budget is deliberately tight so a regression shows up as a failed run.
    http_req_duration: ['p(95)<50'],
    http_req_failed: ['rate<0.01'],
  },
};

// Any bytes will do: the gateway checks the extension and the size, and the
// stub never parses the file. Keeping it inline avoids a binary fixture.
const CV = '%PDF-1.4\n'.repeat(1024);
const JOB_OFFER =
  'Backend engineer. Go, PostgreSQL, Docker and CI/CD. '.repeat(20);

export function setup() {
  if (!TOKEN) {
    throw new Error('TOKEN is required: k6 run -e TOKEN="<access token>" …');
  }
}

export default function () {
  const response = http.post(
    `${BASE_URL}/analyze`,
    {
      cv: http.file(CV, 'candidate.pdf', 'application/pdf'),
      jobOffer: JOB_OFFER,
      jobTitle: 'Backend Engineer',
    },
    { headers: { Authorization: `Bearer ${TOKEN}` }, tags: { route: '/analyze' } },
  );

  check(response, {
    'status is 200': (r) => r.status === 200,
    'body carries a score': (r) => r.json('score') !== undefined,
    'response is correlated': (r) => !!r.headers['X-Request-Id'],
  });
}
