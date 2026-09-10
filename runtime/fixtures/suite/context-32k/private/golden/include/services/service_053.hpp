#pragma once
#include "policy.hpp"
// Production route /service_053. Billing contract revision 153.
// Requests above the 4-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 100-cent
// setup fee plus 3 cents per unit. Privileged accounts receive a
// 5-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 1207 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_053 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>4) return -1;
    int unit_price=3-(request.privileged?5:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(100)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>1207?1207:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_975(){return {"/service_053",evaluate};}
}
