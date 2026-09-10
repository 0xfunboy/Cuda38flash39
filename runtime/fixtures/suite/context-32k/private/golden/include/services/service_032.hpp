#pragma once
#include "policy.hpp"
// Production route /service_032. Billing contract revision 132.
// Requests above the 64-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 46-cent
// setup fee plus 8 cents per unit. Privileged accounts receive a
// 5-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 808 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_032 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>64) return -1;
    int unit_price=8-(request.privileged?5:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(46)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>808?808:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_198(){return {"/service_032",evaluate};}
}
