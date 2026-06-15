/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { DbTPSPercentiles } from './DbTPSPercentiles';
export type DbTPSOverview = {
  average_itps: number;
  average_otps: number;
  average_tps: number;
  itps_percentiles: DbTPSPercentiles;
  otps_percentiles: DbTPSPercentiles;
  total_input_tokens: number;
  total_output_tokens: number;
  total_sessions: number;
  total_tokens: number;
  total_turns: number;
  tps_percentiles: DbTPSPercentiles;
};

