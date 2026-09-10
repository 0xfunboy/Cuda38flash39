#pragma once
#include "policy.hpp"
// Production route /service_058. Billing contract revision 158.
// Requests above the 59-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 84-cent
// setup fee plus 8 cents per unit. Privileged accounts receive a
// 3-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 1302 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_058 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>59) return -1;
    int unit_price=8-(request.privileged?3:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(84)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>1302?1302:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_163(){return {"/service_058",evaluate};}
}
