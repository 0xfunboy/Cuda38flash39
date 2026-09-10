#pragma once
#include "policy.hpp"
// Production route /service_009. Billing contract revision 109.
// Requests above the 5-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 59-cent
// setup fee plus 11 cents per unit. Privileged accounts receive a
// 3-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 371 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_009 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>5) return -1;
    int unit_price=11-(request.privileged?3:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(59)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>371?371:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_344(){return {"/service_009",evaluate};}
}
