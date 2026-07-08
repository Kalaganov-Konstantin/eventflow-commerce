import http from 'k6/http';
import crypto from 'k6/crypto';
import encoding from 'k6/encoding';
import { check, fail, sleep } from 'k6';
import { Rate } from 'k6/metrics';

// BASE_URL points at the api-gateway of a running `make demo` stack. PRODUCT_ID must already
// exist in the inventory catalog with enough stock to absorb the whole run, since there is no
// HTTP endpoint to create one; `make test-performance` seeds it directly in postgres before
// invoking this script.
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const JWT_SECRET = __ENV.JWT_SECRET;
const PRODUCT_ID = __ENV.PRODUCT_ID;

if (!JWT_SECRET) {
  fail('JWT_SECRET env var is required: the same secret the running api-gateway validates against');
}
if (!PRODUCT_ID) {
  fail('PRODUCT_ID env var is required: the id of a pre-seeded, in-stock product');
}

const orderErrors = new Rate('order_errors');

export const options = {
  scenarios: {
    order_flow: {
      executor: 'constant-vus',
      vus: 10,
      duration: '30s',
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500'],
    order_errors: ['rate<0.01'],
  },
};

// signJWT builds an HS256 token with the claims api-gateway's middleware requires: user_id,
// email and role, all non-empty.
function signJWT(customerID) {
  const header = base64url(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
  const payload = base64url(
    JSON.stringify({
      user_id: customerID,
      email: `${customerID}@customers.eventflow.local`,
      role: 'customer',
      exp: Math.floor(Date.now() / 1000) + 3600,
    })
  );
  const signingInput = `${header}.${payload}`;
  const signature = crypto.hmac('sha256', JWT_SECRET, signingInput, 'base64rawurl');
  return `${signingInput}.${signature}`;
}

function base64url(value) {
  return encoding.b64encode(value, 'rawurl');
}

// uuidv4 generates a random-enough id for a distinct load test customer per iteration; it is not
// meant to be cryptographically strong.
function uuidv4() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

export default function () {
  const customerID = uuidv4();
  const token = signJWT(customerID);

  const body = JSON.stringify({
    currency: 'USD',
    items: [
      {
        product_id: PRODUCT_ID,
        product_name: 'Load Test Widget',
        quantity: 1,
        unit_price_cents: 999,
      },
    ],
  });

  const res = http.post(`${BASE_URL}/api/v1/orders`, body, {
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
  });

  const accepted = check(res, {
    'order accepted (202)': (r) => r.status === 202,
  });
  orderErrors.add(!accepted);

  sleep(1);
}
