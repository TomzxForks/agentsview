/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { DbTPSPercentiles } from './DbTPSPercentiles';
export type DbTPSModelStat = {
  average_itps: number;
  average_otps: number;
  average_tps: number;
  itps_percentiles: DbTPSPercentiles;
  model: string;
  otps_percentiles: DbTPSPercentiles;
  total_duration_seconds: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_tokens: number;
  tps_percentiles: DbTPSPercentiles;
  turn_count: number;
};

