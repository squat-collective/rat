import type { Transport } from "../transport";

export class BaseResource {
  protected transport: Transport;

  constructor(transport: Transport) {
    this.transport = transport;
  }
}
