import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';

const BASE_URL = __ENV.WORKLOAD_BASE_URL || 'http://workload:8080';
const MODE = __ENV.K6_MODE || 'mixed';

const mixedStages = [
  { duration: '1m', target: 10 },
  { duration: '3m', target: 10 },
  { duration: '30s', target: 0 },
];

const writeOnlyStages = [
  { duration: '15s', target: 4 },
  { duration: '30s', target: 4 },
  { duration: '15s', target: 0 },
];

const trends = {
  recentOrders: new Trend('endpoint_recent_orders_duration', true),
  inventory: new Trend('endpoint_inventory_duration', true),
  updateStatus: new Trend('endpoint_update_status_duration', true),
  dailySales: new Trend('endpoint_daily_sales_duration', true),
  topCustomers: new Trend('endpoint_top_customers_duration', true),
  logEvents: new Trend('endpoint_log_events_duration', true),
};

export const options =
  MODE === 'write-only'
    ? {
        scenarios: {
          oltpWrites: {
            executor: 'ramping-vus',
            exec: 'writeStatusScenario',
            startVUs: 0,
            stages: writeOnlyStages,
            gracefulRampDown: '5s',
          },
          logWrites: {
            executor: 'ramping-vus',
            exec: 'logEventsScenario',
            startVUs: 0,
            stages: writeOnlyStages,
            gracefulRampDown: '5s',
          },
        },
      }
    : {
        scenarios: {
          oltpRecent: {
            executor: 'ramping-vus',
            exec: 'recentOrdersScenario',
            startVUs: 0,
            stages: mixedStages.map((stage) => ({ ...stage, target: Math.ceil(stage.target * 0.3) })),
            gracefulRampDown: '5s',
          },
          oltpInventory: {
            executor: 'ramping-vus',
            exec: 'inventoryScenario',
            startVUs: 0,
            stages: mixedStages.map((stage) => ({ ...stage, target: Math.ceil(stage.target * 0.2) })),
            gracefulRampDown: '5s',
          },
          oltpWrites: {
            executor: 'ramping-vus',
            exec: 'writeStatusScenario',
            startVUs: 0,
            stages: mixedStages.map((stage) => ({ ...stage, target: Math.ceil(stage.target * 0.2) })),
            gracefulRampDown: '5s',
          },
          olapDailySales: {
            executor: 'ramping-vus',
            exec: 'dailySalesScenario',
            startVUs: 0,
            stages: mixedStages.map((stage) => ({ ...stage, target: Math.max(1, Math.ceil(stage.target * 0.1)) })),
            gracefulRampDown: '5s',
          },
          olapTopCustomers: {
            executor: 'ramping-vus',
            exec: 'topCustomersScenario',
            startVUs: 0,
            stages: mixedStages.map((stage) => ({ ...stage, target: Math.max(1, Math.ceil(stage.target * 0.1)) })),
            gracefulRampDown: '5s',
          },
          logEvents: {
            executor: 'ramping-vus',
            exec: 'logEventsScenario',
            startVUs: 0,
            stages: mixedStages.map((stage) => ({ ...stage, target: Math.ceil(stage.target * 0.2) })),
            gracefulRampDown: '5s',
          },
        },
      };

function record(trend, response) {
  trend.add(response.timings.duration);
  check(response, {
    'status is 200': (r) => r.status === 200,
  });
}

export function recentOrdersScenario() {
  const response = http.get(`${BASE_URL}/api/oltp/orders/recent?limit=20&days=365`, {
    tags: { endpoint: 'recent_orders' },
  });
  record(trends.recentOrders, response);
  sleep(1);
}

export function inventoryScenario() {
  const response = http.get(`${BASE_URL}/api/oltp/inventory/availability?limit=20&search=${encodeURIComponent('Fruit 12')}`, {
    tags: { endpoint: 'inventory' },
  });
  record(trends.inventory, response);
  sleep(1);
}

export function writeStatusScenario() {
  const response = http.post(`${BASE_URL}/api/oltp/orders/status`, null, {
    tags: { endpoint: 'update_status' },
  });
  record(trends.updateStatus, response);
  sleep(0.5);
}

export function dailySalesScenario() {
  const response = http.get(`${BASE_URL}/api/olap/sales/daily?days=365&limit=90`, {
    tags: { endpoint: 'daily_sales' },
  });
  record(trends.dailySales, response);
  sleep(2);
}

export function topCustomersScenario() {
  const response = http.get(`${BASE_URL}/api/olap/customers/top?days=365&limit=20`, {
    tags: { endpoint: 'top_customers' },
  });
  record(trends.topCustomers, response);
  sleep(2);
}

export function logEventsScenario() {
  const response = http.post(`${BASE_URL}/api/logs/events?batch_size=60`, null, {
    tags: { endpoint: 'log_events' },
  });
  record(trends.logEvents, response);
  sleep(0.5);
}
